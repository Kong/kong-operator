package kongintegration

import (
	"os"
	"testing"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestMain(m *testing.M) {
	logger := zapr.NewLogger(zap.New(zapcore.NewNopCore()))
	// Prevents controller-runtime from logging
	// [controller-runtime] log.SetLogger(...) was never called; logs will not be displayed.
	ctrllog.SetLogger(logger)

	os.Exit(m.Run())
}
