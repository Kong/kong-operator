#!/usr/bin/env python3
"""Verify real upstream mTLS, Vault-only storage, rotation, and binding lifecycle.

Required environment: NAMESPACE, BACKEND_NAMESPACE, GATEWAY_NAME, CONTROL_PLANE_NAME,
CONFIG_STORE_NAME, UPSTREAM_HOSTNAME, TEST_LABEL, KONNECT_TOKEN.
Modes: prepare, check (default), rotate, hostname, revoked, legacy, direct, deleted.
MODE=rotate requires BASELINE (the JSON output of MODE=check).
"""

import base64
import contextlib
import hashlib
import http.client
import json
import os
import socket
import ssl
import subprocess
import tempfile
import time
from pathlib import Path

from verify_gateway_vault_certificate import api, get, verify_reference_only_storage


def apply(resource):
    subprocess.run(["kubectl", "apply", "-f", "-"], input=json.dumps(resource).encode(),
                   check=True, stdout=subprocess.DEVNULL, timeout=30)


def patch(kind, name, namespace, body):
    subprocess.run(["kubectl", "-n", namespace, "patch", kind, name, "--type=merge",
                    "-p", json.dumps(body)], check=True, stdout=subprocess.DEVNULL, timeout=30)


def secret_data(name, namespace=None):
    return {key: base64.b64decode(value) for key, value in get("secret", name, namespace)["data"].items()}


def fingerprint(pem):
    leaf = pem.decode().split("-----END CERTIFICATE-----", 1)[0] + "-----END CERTIFICATE-----\n"
    return hashlib.sha256(ssl.PEM_cert_to_DER_cert(leaf)).hexdigest()


def until(check):
    deadline = time.monotonic() + 180
    last_error = "Not ready"
    while time.monotonic() < deadline:
        try:
            return check()
        except (RuntimeError, OSError, http.client.HTTPException) as error:
            last_error = str(error)
        time.sleep(3)
    raise RuntimeError("Upstream mTLS did not converge: " + last_error)


def programmed(obj):
    return any(c["type"] == "Programmed" and c["status"] == "True" and
               c.get("observedGeneration", 0) == obj["metadata"].get("generation", 0)
               for c in obj.get("status", {}).get("conditions", []))


@contextlib.contextmanager
def forward(namespace, service, remote_port):
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    with tempfile.TemporaryFile() as log:
        process = subprocess.Popen([
            "kubectl", "-n", namespace, "port-forward", "service/" + service,
            str(port) + ":" + str(remote_port), "--address=127.0.0.1",
        ], stdout=log, stderr=log)
        try:
            def ready():
                log.seek(0)
                output = log.read().decode()
                if process.poll() is not None:
                    raise AssertionError("Port-forward exited: " + output)
                if "Forwarding from 127.0.0.1:" in output:
                    return port
                raise RuntimeError("Port-forward listener not ready")
            yield until(ready)
        finally:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=15)


def request(port, context=None, hostname=None):
    with contextlib.ExitStack() as stack:
        connection = stack.enter_context(socket.create_connection(("127.0.0.1", port), timeout=10))
        if context:
            connection = stack.enter_context(context.wrap_socket(connection, server_hostname=hostname))
        connection.sendall(("GET / HTTP/1.1\r\nHost: upstream.example.test\r\nConnection: close\r\n\r\n").encode())
        response = http.client.HTTPResponse(connection)
        response.begin()
        return response.status, response.read()


def expect_tls_rejection(port, context, hostname):
    try:
        request(port, context, hostname)
    except (ssl.SSLError, ConnectionResetError):
        return
    raise AssertionError("Upstream accepted an invalid TLS connection")


def verify_upstream_controls():
    backend_namespace = os.environ["BACKEND_NAMESPACE"]
    hostname = os.environ["UPSTREAM_HOSTNAME"]
    server_ca = secret_data("server-ca", backend_namespace)["tls.crt"].decode()
    client_ca = secret_data("client-ca")["tls.crt"].decode()
    context = ssl.create_default_context(cadata=server_ca)
    with forward(backend_namespace, "upstream", 443) as port:
        expect_tls_rejection(port, context, hostname)
    with tempfile.TemporaryDirectory() as directory:
        for secret_name, namespace, valid in (
            ("untrusted-client", backend_namespace, False),
            ("client", os.environ["NAMESPACE"], True),
        ):
            material = secret_data(secret_name, namespace)
            cert_file, key_file = Path(directory) / "cert.pem", Path(directory) / "key.pem"
            cert_file.write_bytes(material["tls.crt"])
            key_file.write_bytes(material["tls.key"])
            context = ssl.create_default_context(cadata=server_ca)
            context.load_cert_chain(cert_file, key_file)
            with forward(backend_namespace, "upstream", 443) as port:
                if not valid:
                    expect_tls_rejection(port, context, hostname)
                    continue
                status, body = request(port, context, hostname)
                assert status == 200 and json.loads(body)["client_fingerprint"] == fingerprint(material["tls.crt"])
            with forward(backend_namespace, "upstream", 443) as port:
                expect_tls_rejection(port, context, "wrong-server.example.test")
            wrong_ca = ssl.create_default_context(cadata=client_ca)
            wrong_ca.load_cert_chain(cert_file, key_file)
            with forward(backend_namespace, "upstream", 443) as port:
                expect_tls_rejection(port, wrong_ca, hostname)


def external_certificate():
    cert = get("kongcertificate", "client", os.environ["BACKEND_NAMESPACE"])
    assert not cert["metadata"].get("ownerReferences"), "External certificate was adopted"
    assert not cert["metadata"].get("labels"), "External certificate was relabeled"
    assert not any(key.startswith("gateway-operator.konghq.com/hybrid-")
                   for key in cert["metadata"].get("annotations", {})), "External certificate received ownership annotations"
    return cert


def generated_grants():
    return [grant for grant in get("kongreferencegrants", "", os.environ["BACKEND_NAMESPACE"])["items"]
            if any(key.startswith("gateway-operator.konghq.com/hybrid-")
                   for key in grant["metadata"].get("annotations", {}))]


def snapshot(service, cert):
    pods = [p for p in get("pods", "")["items"] if
            p["metadata"].get("labels", {}).get("upstream-mtls-test") == os.environ["TEST_LABEL"]]
    assert pods, "No DataPlane Pods"
    return {
        "certificate": {"id": cert["status"]["konnect"]["id"], "spec": cert["spec"]},
        "service": {"id": service["status"]["konnect"]["id"], "spec": service["spec"]},
        "pods": sorted([p["metadata"]["uid"], sorted(
            [c["name"], c["restartCount"]] for c in p["status"].get("containerStatuses", [])
        )] for p in pods),
    }


def binding(cp_id, legacy=False):
    services = get("kongservices", "")["items"]
    if len(services) != 1 or not programmed(services[0]):
        raise RuntimeError("Expected one currently programmed KongService")
    service = services[0]
    cert = external_certificate()
    if legacy:
        generated = get("kongcertificates", "")["items"]
        if len(generated) != 1 or not programmed(generated[0]):
            raise RuntimeError("Legacy certificate not programmed")
        selected = generated[0]
    else:
        selected = cert
        if get("kongcertificates", "")["items"]:
            raise RuntimeError("Obsolete generated certificates not cleaned up")
        if generated_grants():
            raise RuntimeError("Obsolete generated Secret authorization grants not cleaned up")
    ref = service["spec"].get("clientCertificateRef", {})
    if ref.get("name") != selected["metadata"]["name"] or ref.get("namespace", service["metadata"]["namespace"]) != selected["metadata"]["namespace"]:
        raise RuntimeError("KongService has not selected the desired certificate")
    cert_id = selected.get("status", {}).get("konnect", {}).get("id")
    if not cert_id or service["status"]["konnect"].get("certificateID") != cert_id:
        raise RuntimeError("Certificate binding not programmed")
    remote = api("/v2/control-planes/" + cp_id + "/core-entities/services/" + service["status"]["konnect"]["id"])
    if (remote.get("client_certificate") or {}).get("id") != cert_id:
        raise RuntimeError("Remote Service certificate binding has not converged")
    assert remote["tls_verify"] is True, "Upstream server verification is disabled"
    return service, cert


def verified_proxy(port, expected):
    status, body = request(port)
    if status != 200:
        raise RuntimeError("Proxy returned HTTP " + str(status))
    result = json.loads(body)
    if result["client_fingerprint"] != expected:
        raise RuntimeError("New upstream TLS handshake used an old client certificate")
    assert result["server_name"] == os.environ["UPSTREAM_HOSTNAME"], "Incorrect upstream SNI"
    return result


def prepare():
    namespace, backend = os.environ["NAMESPACE"], os.environ["BACKEND_NAMESPACE"]
    for ca_name, source, target in (
        ("server", backend, namespace), ("client", namespace, backend),
    ):
        apply({"apiVersion": "v1", "kind": "ConfigMap",
               "metadata": {"name": "upstream-" + ca_name + "-ca", "namespace": target},
               "data": {"ca.crt": secret_data(ca_name + "-ca", source)["tls.crt"].decode()}})


def main():
    mode = os.environ.get("MODE", "check")
    if mode not in {"prepare", "check", "rotate", "hostname", "revoked", "legacy", "direct", "deleted"}:
        raise ValueError("Unsupported verification mode: " + mode)
    if mode == "prepare":
        prepare()
        return
    namespace, backend = os.environ["NAMESPACE"], os.environ["BACKEND_NAMESPACE"]
    cp_id = get("konnectgatewaycontrolplane", os.environ["CONTROL_PLANE_NAME"])["status"]["id"]
    store_id = get("konnectconfigstore", os.environ["CONFIG_STORE_NAME"])["status"]["id"]
    cert = external_certificate()
    if mode in ("revoked", "deleted"):
        def absent():
            if get("kongservices", "")["items"] or get("kongroutes", "")["items"]:
                raise RuntimeError("Old route/service bindings still exist")
            if get("kongcertificates", "")["items"] or generated_grants():
                raise RuntimeError("Old generated certificates or Secret grants still exist")
            for kind in ("services", "routes"):
                if api("/v2/control-planes/" + cp_id + "/core-entities/" + kind)["data"]:
                    raise RuntimeError("Old remote bindings still exist")
        until(absent)
        current_cert = external_certificate()
        assert current_cert["metadata"]["uid"] == cert["metadata"]["uid"], "External Certificate was recreated"
        assert current_cert["status"]["konnect"]["id"] == cert["status"]["konnect"]["id"], "External Certificate ID changed"
        verify_reference_only_storage(cp_id, store_id, current_cert)
        print(json.dumps({"external_certificate_id": cert["status"]["konnect"]["id"]}))
        return
    if mode == "legacy":
        material = get("secret", "client")["data"]
        apply({"apiVersion": "v1", "kind": "Secret", "metadata": {
            "name": "legacy-client", "namespace": backend, "labels": {"konghq.com/secret": "true"},
        }, "type": "kubernetes.io/tls", "data": material})
        patch("service", "upstream", backend, {"metadata": {"annotations": {
            "konghq.com/client-cert-ref": None, "konghq.com/client-cert": "legacy-client",
        }}})
    elif mode == "direct":
        patch("service", "upstream", backend, {"metadata": {"annotations": {
            "konghq.com/client-cert": None, "konghq.com/client-cert-ref": backend + "/client",
        }}})
    baseline = json.loads(os.environ["BASELINE"]) if mode == "rotate" else None
    if baseline:
        patch("certificate", "client", namespace, {"spec": {"commonName": "upstream-client-rotated"}})
    if mode == "check":
        verify_upstream_controls()
    dataplanes = get("dataplanes", "")["items"]
    gateway = get("gateway", os.environ["GATEWAY_NAME"])
    dataplanes = [d for d in dataplanes if any(
        r["uid"] == gateway["metadata"]["uid"] for r in d["metadata"].get("ownerReferences", [])
    )]
    if len(dataplanes) != 1 or not dataplanes[0].get("status", {}).get("service"):
        raise RuntimeError("Gateway DataPlane ingress Service not ready")
    with forward(namespace, dataplanes[0]["status"]["service"], 80) as port:
        if mode == "hostname":
            patch("service", "upstream", backend, {"metadata": {"annotations": {
                "konghq.com/host-header": "wrong-server.example.test",
            }}})
            try:
                def rejected():
                    upstreams = get("kongupstreams", "")["items"]
                    if len(upstreams) != 1 or not programmed(upstreams[0]):
                        raise RuntimeError("Waiting for upstream update")
                    upstream_id = upstreams[0]["status"]["konnect"]["id"]
                    remote = api("/v2/control-planes/" + cp_id + "/core-entities/upstreams/" + upstream_id)
                    if remote["host_header"] != "wrong-server.example.test":
                        raise RuntimeError("Wrong hostname not yet programmed")
                    if request(port)[0] != 502:
                        raise RuntimeError("Gateway did not reject the wrong upstream server hostname")
                until(rejected)
            finally:
                patch("service", "upstream", backend, {"metadata": {"annotations": {
                    "konghq.com/host-header": os.environ["UPSTREAM_HOSTNAME"],
                }}})
        def verified():
            service, current_cert = binding(cp_id, mode == "legacy")
            assert current_cert["metadata"]["uid"] == cert["metadata"]["uid"], "External Certificate was recreated"
            assert current_cert["status"]["konnect"]["id"] == cert["status"]["konnect"]["id"], "External Certificate ID changed"
            expected = fingerprint(secret_data("client")["tls.crt"])
            if baseline and expected == baseline["fingerprint"]:
                raise RuntimeError("cert-manager has not rotated the client certificate")
            verified_proxy(port, expected)
            if mode != "legacy":
                remote = api("/v2/control-planes/" + cp_id + "/core-entities/certificates")["data"]
                if len(remote) != 1:
                    raise RuntimeError("Obsolete remote certificates not cleaned up")
                verify_reference_only_storage(cp_id, store_id, current_cert)
            else:
                remote = api("/v2/control-planes/" + cp_id + "/core-entities/certificates/" + cert["status"]["konnect"]["id"])
                assert all(remote[field] == cert["spec"][field] for field in ("cert", "key")), "External Certificate mutated"
            current = snapshot(service, current_cert)
            if baseline:
                assert current == baseline["snapshot"], "Certificate/Service IDs/specs or DataPlane Pods changed during rotation"
            return {"fingerprint": expected, "snapshot": current}
        print(json.dumps(until(verified)))


if __name__ == "__main__":
    main()
