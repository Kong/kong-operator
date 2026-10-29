#!/usr/bin/env python3
"""Exercise the actual Chainsaw upstream and TLS verifier without a cluster."""

import contextlib
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import tempfile
import time
import unittest
from unittest import mock

import verify_upstream_vault_certificate as verifier
from verify_upstream_vault_certificate import expect_tls_rejection, fingerprint, request


class UpstreamMTLSTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.directory.cleanup)
        cls.root = Path(cls.directory.name)

        def openssl(*args):
            subprocess.run(["openssl", *args], cwd=cls.root, check=True,
                           stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=30)

        (cls.root / "ca.conf").write_text(
            "[req]\ndistinguished_name=dn\n[dn]\n[v3_ca]\n"
            "basicConstraints=critical,CA:TRUE\nkeyUsage=critical,keyCertSign,cRLSign\nsubjectKeyIdentifier=hash\n"
        )
        for ca in ("client-ca", "server-ca"):
            openssl("req", "-new", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                    "-subj", "/CN=" + ca, "-keyout", ca + ".key", "-out", ca + ".crt",
                    "-config", "ca.conf", "-extensions", "v3_ca")
        for name, ca, usage in (
            ("server", "server-ca", "serverAuth"), ("client", "client-ca", "clientAuth"),
            ("rotated", "client-ca", "clientAuth"), ("untrusted", "server-ca", "clientAuth"),
        ):
            openssl("req", "-new", "-newkey", "rsa:2048", "-nodes", "-subj", "/CN=" + name,
                    "-keyout", name + ".key", "-out", name + ".csr")
            (cls.root / "extensions").write_text(
                "basicConstraints=critical,CA:FALSE\nsubjectKeyIdentifier=hash\n"
                "authorityKeyIdentifier=keyid,issuer\nextendedKeyUsage=" + usage +
                "\nsubjectAltName=DNS:upstream.example.test\n"
            )
            openssl("x509", "-req", "-in", name + ".csr", "-CA", ca + ".crt", "-CAkey",
                    ca + ".key", "-CAcreateserial", "-days", "1", "-extfile", "extensions",
                    "-out", name + ".crt")
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            cls.port = reservation.getsockname()[1]
        server = Path(__file__).resolve().parents[2] / "hybridgateway/httproute-kongcertificate/upstream_server.py"
        cls.process = subprocess.Popen(["python3", str(server)], env={
            **os.environ, "PORT": str(cls.port), "TLS_CERT": str(cls.root / "server.crt"),
            "TLS_KEY": str(cls.root / "server.key"), "CLIENT_CA": str(cls.root / "client-ca.crt"),
        }, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)

        def stop():
            cls.process.terminate()
            try:
                cls.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                cls.process.kill()
                cls.process.wait(timeout=10)
            cls.process.stderr.close()
        cls.addClassCleanup(stop)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if cls.process.poll() is not None:
                raise RuntimeError(cls.process.stderr.read().decode())
            try:
                with socket.create_connection(("127.0.0.1", cls.port), timeout=1):
                    return
            except OSError:
                time.sleep(0.1)
        raise RuntimeError("Test upstream did not start")

    def context(self, client=None, ca="server-ca"):
        context = ssl.create_default_context(cafile=str(self.root / (ca + ".crt")))
        if client:
            context.load_cert_chain(self.root / (client + ".crt"), self.root / (client + ".key"))
        return context

    def test_requires_trusted_client(self):
        for client in (None, "untrusted"):
            with self.subTest(client=client):
                expect_tls_rejection(self.port, self.context(client), "upstream.example.test")

    def test_server_trust_and_hostname_are_independent(self):
        expect_tls_rejection(self.port, self.context("client"), "wrong.example.test")
        expect_tls_rejection(self.port, self.context("client", "client-ca"), "upstream.example.test")

    def test_new_handshakes_observe_rotated_client(self):
        observed = []
        for client in ("client", "rotated"):
            status, body = request(self.port, self.context(client), "upstream.example.test")
            result = json.loads(body)
            self.assertEqual(status, 200)
            self.assertEqual(result["server_name"], "upstream.example.test")
            self.assertEqual(result["client_fingerprint"], fingerprint((self.root / (client + ".crt")).read_bytes()))
            observed.append(result["client_fingerprint"])
        self.assertNotEqual(*observed)

    def test_all_upstream_controls_use_independent_forwards(self):
        def material(name, namespace=None):
            name = {"untrusted-client": "untrusted"}.get(name, name)
            return {"tls.crt": (self.root / (name + ".crt")).read_bytes(),
                    "tls.key": (self.root / (name + ".key")).read_bytes()}
        with mock.patch.dict(os.environ, {
            "NAMESPACE": "gateways", "BACKEND_NAMESPACE": "backends",
            "UPSTREAM_HOSTNAME": "upstream.example.test",
        }), mock.patch.object(verifier, "secret_data", side_effect=material), \
                mock.patch.object(verifier, "forward", side_effect=lambda *args: contextlib.nullcontext(self.port)) as forward:
            verifier.verify_upstream_controls()
            self.assertEqual(forward.call_count, 5)


class VerifierContractTest(unittest.TestCase):
    def test_forward_readiness_does_not_open_an_unauthenticated_tls_connection(self):
        process = mock.Mock()
        process.poll.return_value = None

        def start(args, stdout, stderr):
            stdout.write(b"Forwarding from 127.0.0.1:12345 -> 8443\n")
            stdout.flush()
            return process

        with mock.patch.object(verifier.subprocess, "Popen", side_effect=start), \
                mock.patch.object(verifier.socket, "create_connection", side_effect=AssertionError("Unexpected TCP probe")):
            with verifier.forward("backends", "upstream", 443) as port:
                self.assertIsInstance(port, int)
        process.terminate.assert_called_once()
        process.wait.assert_called_once_with(timeout=15)

    def test_unknown_mode_is_not_a_successful_check(self):
        with mock.patch.dict(os.environ, {"MODE": "rotte"}):
            with self.assertRaisesRegex(ValueError, "Unsupported verification mode"):
                verifier.main()

    def test_all_hybrid_tracking_annotations_are_rejected(self):
        for suffix in ("hybrid-gateways", "hybrid-routes.httproute", "hybrid-routes-httproute"):
            with self.subTest(suffix=suffix), mock.patch.dict(os.environ, {"BACKEND_NAMESPACE": "backends"}):
                certificate = {"metadata": {"annotations": {
                    "gateway-operator.konghq.com/" + suffix: "default/route",
                }}}
                with mock.patch.object(verifier, "get", return_value=certificate):
                    with self.assertRaisesRegex(AssertionError, "ownership annotations"):
                        verifier.external_certificate()


if __name__ == "__main__":
    unittest.main()
