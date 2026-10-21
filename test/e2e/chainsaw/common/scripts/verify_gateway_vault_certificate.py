#!/usr/bin/env python3
"""Verify Listener TLS, reference-only Konnect storage, and cert-manager rotation.

Required environment: NAMESPACE, GATEWAY_NAME, CERTIFICATE_NAME,
CERTIFICATE_NAMESPACE, MATERIAL_SECRET_NAME, CA_SECRET_NAME, CONFIG_STORE_NAME,
CONTROL_PLANE_NAME, HOSTNAME, TEST_LABEL, KONNECT_TOKEN.
MODE=rotate additionally requires BASELINE (the JSON output of MODE=check).
KONNECT_SERVER_URL defaults to eu.api.konghq.tech.
"""

import base64
import hashlib
import http.client
import json
import os
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.request


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
            deadline = time.monotonic() + 180
            last_error = "TLS not ready"
            while time.monotonic() < deadline:
                if forward.poll() is not None:
                    log.seek(0)
                    raise RuntimeError("Port-forward exited: " + log.read().decode())
                try:
                    fingerprint, status = proxy_request(context, port, os.environ["HOSTNAME"])
                    if status != 200:
                        raise RuntimeError("Proxy returned HTTP " + str(status))
                    pem = base64.b64decode(get("secret", secret_name)["data"]["tls.crt"]).decode()
                    leaf = pem.split("-----END CERTIFICATE-----", 1)[0] + "-----END CERTIFICATE-----\n"
                    expected = hashlib.sha256(ssl.PEM_cert_to_DER_cert(leaf)).hexdigest()
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
                    last_error = "Proxy certificate does not match the newly issued Secret"
                except (OSError, http.client.HTTPException, RuntimeError) as error:
                    last_error = str(error)
                time.sleep(3)
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
