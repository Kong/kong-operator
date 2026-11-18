package fixtures

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	l4ConnectivityScript = "../common/scripts/host_connectivity_test_l4.sh"
	// go-echo sends several lines, with the probe's default match after the
	// first one.
	l4EchoResponse = "Welcome, you are connected to node test-node.\nRunning on Pod test-echo.\nIn namespace test.\n"
)

type l4ConnectivityResult struct {
	Success      bool   `json:"success"`
	Protocol     string `json:"protocol"`
	RetryAttempt int    `json:"retry_attempt"`
	LastStatus   int    `json:"last_status"`
}

func runL4ConnectivityTest(t *testing.T, protocol string, port int, path string) (l4ConnectivityResult, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", l4ConnectivityScript)
	cmd.Env = append(os.Environ(),
		"PATH="+path,
		"PROTOCOL="+protocol,
		"PROXY_IP=127.0.0.1",
		"PROXY_PORT="+strconv.Itoa(port),
		"MAX_RETRIES=1",
		"RETRY_DELAY=0",
	)
	out, err := cmd.Output()
	var result l4ConnectivityResult
	require.NoError(t, json.Unmarshal(out, &result), "output: %s", out)
	return result, err
}

func startTCPEchoServer(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	done := make(chan struct{})
	t.Cleanup(func() { close(done) })

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte(l4EchoResponse))
		// Keep the connection open: the probe must not wait for EOF.
		<-done
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

func startUDPEchoServer(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 1024)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if n > 0 {
				_, _ = conn.WriteTo([]byte(l4EchoResponse), addr)
				_, _ = conn.WriteTo(buf[:n], addr)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func TestHostConnectivityL4WithoutGNUTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script relies on bash /dev/tcp and /dev/udp")
	}

	// A fake timeout earlier in PATH fails the test if the script still shells
	// out to GNU timeout, which is what breaks on macOS.
	shimDir := t.TempDir()
	marker := filepath.Join(shimDir, "timeout-called")
	require.NoError(t, os.WriteFile(filepath.Join(shimDir, "timeout"), //nolint:gosec // Executable fake timeout in t.TempDir.
		[]byte("#!/bin/sh\nprintf called >\"$TIMEOUT_MARKER\"\nexit 127\n"), 0o700))
	t.Setenv("TIMEOUT_MARKER", marker)
	path := shimDir + string(os.PathListSeparator) + os.Getenv("PATH")

	for _, tc := range []struct {
		protocol string
		port     int
	}{
		{protocol: "tcp", port: startTCPEchoServer(t)},
		{protocol: "udp", port: startUDPEchoServer(t)},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			result, err := runL4ConnectivityTest(t, tc.protocol, tc.port, path)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, tc.protocol, result.Protocol)
			require.Equal(t, 1, result.RetryAttempt)
		})
	}

	require.NoFileExists(t, marker, "the script must not invoke the external timeout command")
}

func TestHostConnectivityL4ReportsFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script relies on bash /dev/tcp and /dev/udp")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())

	result, err := runL4ConnectivityTest(t, "tcp", port, os.Getenv("PATH"))
	require.Error(t, err)
	require.False(t, result.Success)
	require.NotZero(t, result.LastStatus)
}
