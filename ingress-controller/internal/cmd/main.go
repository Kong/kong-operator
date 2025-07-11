package main

import (
	"github.com/kong/kong-operator/ingress-controller/internal/cmd/rootcmd"
)

//go:generate go run github.com/kong/kong-operator/ingress-controller/hack/generators/controllers/networking

func main() {
	rootcmd.Execute()
}
