/*
Copyright 2022 Kong Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	"github.com/kong/kong-operator/v2/modules/cli"
	"github.com/kong/kong-operator/v2/modules/manager"
	"github.com/kong/kong-operator/v2/modules/manager/metadata"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func main() {
	m := metadata.Metadata()

	cli := cli.New(m)
	cfg := cli.Parse(os.Args[1:])

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(cfg.LoggerOpts)))
	secrets.SetCALogger(ctrl.Log)

	if err := manager.Run(cfg, scheme.Get(), manager.SetupControllers, nil, m); err != nil {
		ctrl.Log.Error(err, "failed to run manager")
		os.Exit(1)
	}
}
