#!/usr/bin/env python3
"""Verify Listener TLS, reference-only Konnect storage, and cert-manager rotation.

Required environment: NAMESPACE, GATEWAY_NAME, CERTIFICATE_NAME,
CERTIFICATE_NAMESPACE, MATERIAL_SECRET_NAME, CA_SECRET_NAME, CONFIG_STORE_NAME,
CONTROL_PLANE_NAME, HOSTNAME, TEST_LABEL, KONNECT_TOKEN.
MODE=rotate additionally requires BASELINE (the JSON output of MODE=check).
KONNECT_SERVER_URL defaults to eu.api.konghq.tech.

stdout carries only the JSON result. Progress and failure diagnostics go to stderr.
"""

import base64
import hashlib
import http.client
import json
import os
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.request


def trace(message):
    print(time.strftime("%H:%M:%S") + " " + message, file=sys.stderr, flush=True)


def describe_certificate(der):
    """Return subject, issuer, validity, SANs and fingerprint of a DER certificate."""
    fingerprint = hashlib.sha256(der).hexdigest()
    try:
        details = subprocess.run([
            "openssl", "x509", "-inform", "DER", "-noout", "-subject", "-issuer",
            "-dates", "-ext", "subjectAltName",
        ], input=der, check=True, capture_output=True, timeout=10).stdout.decode().strip()
    except (OSError, subprocess.SubprocessError) as error:
        details = "openssl failed: " + str(error)
    return details + "\nsha256 fingerprint=" + fingerprint


def served_certificate(port):
    """Return a description of the certificate served for HOSTNAME, without verifying it."""
    context = ssl.create_default_context()
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=10) as raw:
            with context.wrap_socket(raw, server_hostname=os.environ["HOSTNAME"]) as tls:
                return describe_certificate(tls.getpeercert(binary_form=True))
    except (OSError, ssl.SSLError) as error:
        return "unavailable: " + str(error)


def pem_leaf(pem):
    return pem.split("-----END CERTIFICATE-----", 1)[0] + "-----END CERTIFICATE-----\n"


def report_failure(port, ca, secret_name, attempts, last_error, forward_log):
    """Print what was served and what was expected, so a timeout shows which side lagged."""
    sections = [
        ("Last error after " + str(attempts) + " attempts", last_error),
        ("Certificate served for SNI " + os.environ["HOSTNAME"] + " (unverified)", served_certificate(port)),
    ]
    try:
        pem = base64.b64decode(get("secret", secret_name)["data"]["tls.crt"]).decode()
        expected = describe_certificate(ssl.PEM_cert_to_DER_cert(pem_leaf(pem)))
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        expected = "unavailable: " + str(error)
    sections.append(("Expected leaf from Secret " + secret_name, expected))
    sections.append(("Trusted CA", describe_certificate(ssl.PEM_cert_to_DER_cert(pem_leaf(ca)))))
    sections.append(("kubectl port-forward output", forward_log.strip() or "(empty)"))
    for title, body in sections:
        print("=== " + title + " ===\n" + body, file=sys.stderr, flush=True)


def get(kind, name, namespace=None):
    return json.loads(subprocess.check_output([
        "kubectl", "-n", namespace or os.environ["NAMESPACE"],
        "get", kind, *([name] if name else []), "-o", "json",
    ], timeout=30))


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def api(path):
    server = os.environ.get("KONNECT_SERVER_URL", "eu.api.konghq.tech").removeprefix("https://").rstrip("/")
    request = urllib.request.Request("https://" + server + path, headers={
        "Authorization": "Bearer " + os.environ["KONNECT_TOKEN"],
    })
    with urllib.request.build_opener(NoRedirect).open(request, timeout=25) as response:
        return json.load(response)


def verify_reference_only_storage(cp_id, store_id, certificate):
    certificates = api("/v2/control-planes/" + cp_id + "/core-entities/certificates")["data"]
    assert len(certificates) == 1, "Expected only the explicitly managed Certificate in Konnect"
    for field in ("cert", "key"):
        assert certificates[0][field] == certificate["spec"][field]
        assert certificates[0][field].startswith("{vault://"), "Plaintext Certificate material uploaded"
    entries = api("/v2/control-planes/" + cp_id + "/config-stores/" + store_id + "/secrets")["data"]
    assert len(entries) == 1 and all("value" not in e for e in entries), "Expected metadata-only Config Store reads"


def snapshot(gateway, certificate):
    namespace = os.environ["NAMESPACE"]
    snis = get("kongsnis", "")["items"]
    snis = [s for s in snis if any(
        r["uid"] == gateway["metadata"]["uid"]
        for r in s["metadata"].get("ownerReferences", [])
    )]
    assert len(snis) == 1, "Expected one Gateway-owned SNI"
    sni = snis[0]
    assert sni["spec"]["name"] == os.environ["HOSTNAME"], "Listener hostname was not preserved"
    assert sni["spec"]["certificateRef"] == {
        "name": certificate["metadata"]["name"],
        "namespace": certificate["metadata"]["namespace"],
    }
    assert not certificate["metadata"].get("ownerReferences"), "External certificate was adopted"
    assert not certificate["metadata"].get("annotations", {}).get(
        "gateway-operator.konghq.com/hybrid-gateways"
    ), "External certificate received controller tracking annotations"
    assert not get("kongcertificates", "")["items"], "Gateway namespace contains a generated Certificate"
    pods = json.loads(subprocess.check_output([
        "kubectl", "-n", namespace, "get", "pods", "-l",
        "tls-listener-test=" + os.environ["TEST_LABEL"], "-o", "json",
    ], timeout=30))["items"]
    assert pods, "No DataPlane Pods found"
    return {
        "certificate": {"id": certificate["status"]["konnect"]["id"], "spec": certificate["spec"]},
        "sni": {"id": sni["status"]["konnect"]["id"], "spec": sni["spec"]},
        "pods": sorted((p["metadata"]["uid"], sum(
            c["restartCount"] for c in p["status"].get("containerStatuses", [])
        )) for p in pods),
    }


def proxy_request(context, port, host):
    with socket.create_connection(("127.0.0.1", port), timeout=10) as raw:
        with context.wrap_socket(raw, server_hostname=os.environ["HOSTNAME"]) as tls:
            fingerprint = hashlib.sha256(tls.getpeercert(binary_form=True)).hexdigest()
            tls.sendall(("GET / HTTP/1.1\r\nHost: " + host +
                         "\r\nConnection: close\r\n\r\n").encode())
            response = http.client.HTTPResponse(tls)
            response.begin()
            response.read()
            return fingerprint, response.status


def main():
    gateway = get("gateway", os.environ["GATEWAY_NAME"])
    certificate = get("kongcertificate", os.environ["CERTIFICATE_NAME"], os.environ["CERTIFICATE_NAMESPACE"])
    cp_id = get("konnectgatewaycontrolplane", os.environ["CONTROL_PLANE_NAME"])["status"]["id"]
    store_id = get("konnectconfigstore", os.environ["CONFIG_STORE_NAME"])["status"]["id"]
    verify_reference_only_storage(cp_id, store_id, certificate)

    baseline = json.loads(os.environ["BASELINE"]) if os.environ.get("MODE", "check") == "rotate" else None
    secret_name = os.environ["MATERIAL_SECRET_NAME"]
    if baseline:
        subprocess.run(["kubectl", "-n", os.environ["NAMESPACE"], "patch", "certificate", secret_name,
                        "--type=merge", "-p", json.dumps({"spec": {
                            "dnsNames": [os.environ["HOSTNAME"], "rotation." + os.environ["HOSTNAME"]]
                        }})], check=True, stdout=subprocess.DEVNULL, timeout=30)
    ca = base64.b64decode(get("secret", os.environ["CA_SECRET_NAME"])["data"]["tls.crt"]).decode()
    context = ssl.create_default_context(cadata=ca)
    dataplanes = get("dataplanes", "")["items"]
    dataplanes = [d for d in dataplanes if any(
        r["uid"] == gateway["metadata"]["uid"]
        for r in d["metadata"].get("ownerReferences", [])
    )]
    assert len(dataplanes) == 1, "Expected one Gateway-owned DataPlane"
    service = dataplanes[0]["status"]["service"]
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    with tempfile.TemporaryFile() as log:
        forward = subprocess.Popen([
            "kubectl", "-n", os.environ["NAMESPACE"], "port-forward",
            "service/" + service, str(port) + ":443", "--address=127.0.0.1",
        ], stdout=log, stderr=log)
        try:
            started = time.monotonic()
            deadline = started + 180
            last_error = "TLS not ready"
            logged_error = None
            attempts = 0
            trace("Checking Listener TLS for " + os.environ["HOSTNAME"] + " via service/" + service +
                " (mode " + os.environ.get("MODE", "check") + ")")
            while time.monotonic() < deadline:
                if forward.poll() is not None:
                    log.seek(0)
                    raise RuntimeError("Port-forward exited: " + log.read().decode())
                if last_error != logged_error:
                    # Log each change of state once, so the timeline shows whether TLS ever progressed.
                    trace("attempt " + str(attempts) + " after " + str(int(time.monotonic() - started)) +
                        "s: " + last_error)
                    logged_error = last_error
                attempts += 1
                try:
                    fingerprint, status = proxy_request(context, port, os.environ["HOSTNAME"])
                    if status != 200:
                        raise RuntimeError("Proxy returned HTTP " + str(status))
                    pem = base64.b64decode(get("secret", secret_name)["data"]["tls.crt"]).decode()
                    expected = hashlib.sha256(ssl.PEM_cert_to_DER_cert(pem_leaf(pem))).hexdigest()
                    if fingerprint == expected and (not baseline or fingerprint != baseline["fingerprint"]):
                        _, unmatched_status = proxy_request(context, port, "unmatched.example.test")
                        assert unmatched_status == 404, "HTTPRoute did not inherit the Listener hostname"
                        certificate = get("kongcertificate", os.environ["CERTIFICATE_NAME"],
                                          os.environ["CERTIFICATE_NAMESPACE"])
                        verify_reference_only_storage(cp_id, store_id, certificate)
                        current = snapshot(gateway, certificate)
                        # JSON normalizes tuple/list representation before comparison.
                        if baseline:
                            assert json.loads(json.dumps(current)) == baseline["snapshot"], "Entities or Pods changed during rotation"
                        print(json.dumps({"fingerprint": fingerprint, "snapshot": current}))
                        return
                    last_error = ("Proxy certificate " + fingerprint[:16] + " does not match the newly issued "
                                  "Secret leaf " + expected[:16] +
                                  (" (baseline " + baseline["fingerprint"][:16] + ")" if baseline else ""))
                except (OSError, http.client.HTTPException, RuntimeError) as error:
                    last_error = str(error)
                time.sleep(3)
            log.seek(0)
            report_failure(port, ca, secret_name, attempts, last_error, log.read().decode(errors="replace"))
            raise RuntimeError("Verified Listener TLS did not converge: " + last_error)
        finally:
            forward.terminate()
            try:
                forward.wait(timeout=15)
            except subprocess.TimeoutExpired:
                forward.kill()
                forward.wait(timeout=15)


if __name__ == "__main__":
    main()
