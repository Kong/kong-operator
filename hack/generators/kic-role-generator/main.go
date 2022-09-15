package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	rbacv1 "k8s.io/api/rbac/v1"

	kicversions "github.com/kong/gateway-operator/internal/versions"
)

const gitClonePath = "./kubernetes-ingress-controller"

var clusterRoleRelativePaths = []string{
	"config/rbac/role.yaml",
	"config/rbac/gateway/role.yaml",
	"config/rbac/knative/role.yaml",
}

const (
	controllerRBACPath       = "./controllers/versioned_clusterroles"
	controllerRBACFilePrefix = "zz_generated_kong_ingress_controller_rbac"

	kicRBACPath       = "./internal/utils/kubernetes/resources/clusterroles"
	kicRBACFIlePrefix = "zz_generated_controlplane_clusterrole"

	kicRBACHelperFileName = "./internal/utils/kubernetes/resources/zz_generated_clusterrole_helpers.go"
)

var (
	dryRun      bool
	failOnError bool
	force       bool
)

func init() {
	flag.BoolVar(&dryRun, "dry-run", false, "Only check if the existing files are up to date.")
	flag.BoolVar(&failOnError, "fail-on-error", false, "Exit with error if the existing files are not up to date.")
	flag.BoolVar(&force, "force", false, "force the regeneration of files")
	flag.Parse()
}

func main() {
	gatewayRepo, err := git.PlainOpen(".")
	exitOnErr(err)

	gatewayWorktree, err := gatewayRepo.Worktree()
	exitOnErr(err)

	kicSubmodule, err := gatewayWorktree.Submodule("kubernetes-ingress-controller")
	exitOnErr(err)
	kicStatus, err := kicSubmodule.Status()
	exitOnErr(err)
	prevHead := kicStatus.Current
	if !kicStatus.IsClean() {
		exitOnErr(
			fmt.Errorf("status of kubernetes-ingress-controller submodule is not clean: %s", kicStatus))
	}

	err = kicSubmodule.UpdateContext(context.Background(),
		&git.SubmoduleUpdateOptions{
			Init: true,
		},
	)
	exitOnErr(err)
	kicRepo, err := kicSubmodule.Repository()
	exitOnErr(err)

	kicWorktree, err := kicRepo.Worktree()
	exitOnErr(err)

	if force {
		exitOnErr(rmDirs(controllerRBACPath, kicRBACPath))
	}

	// defer reverting KIC's submodule back to status from before.
	defer checkout(kicWorktree, prevHead)
	for c, v := range kicversions.RoleVersionsForKICVersions {
		fmt.Printf("INFO: checking and generating code for constraint %s with version %s\n", c, v)
		// ensure the version has the "v" prefix
		version := semver.MustParse(v).String()
		if !strings.HasPrefix(version, "v") {
			version = fmt.Sprintf("v%s", version)
		}
		fmt.Printf("INFO: checking out tag %s\n", version)
		exitOnErr(gitCheckoutTag(kicRepo, kicWorktree, version))

		fmt.Printf("INFO: parsing clusterRole for KIC version %s\n", version)
		clusterRoles := []*rbacv1.ClusterRole{}
		for _, rolePath := range clusterRoleRelativePaths {
			// Here we try to merge all the rules from all known cluster roles.
			rolePath := path.Join(gitClonePath, rolePath)
			if _, err = os.Stat(rolePath); errors.Is(err, os.ErrNotExist) {
				continue
			}
			newRole, err := parseRole(rolePath)
			exitOnErr(err)
			clusterRoles = append(clusterRoles, newRole)
		}

		exitOnErr(generatefile(
			clusterRoles,
			c,
			"kic-rbac",
			kicRBACTemplate,
			kicRBACPath,
			kicRBACFIlePrefix,
		))

		exitOnErr(generatefile(
			clusterRoles,
			c,
			"controller-annotations",
			controlplaneControllerRBACTemplate,
			controllerRBACPath,
			controllerRBACFilePrefix,
		))
	}

	buffer, err := renderHelperTemplate(kicversions.RoleVersionsForKICVersions, "kic-rbac", kicRBACHelperTemplate)
	exitOnErr(err)
	m, err := filesEqual(kicRBACHelperFileName, buffer)
	exitOnErr(err)
	if !m {
		if failOnError {
			exitOnErr(fmt.Errorf("KIC rbac helper out of date, please regenerate it"))
		}
		fmt.Println("INFO: KIC rbac helper out of date, needs to be regenerated")
		if !dryRun {
			fmt.Println("INFO: regenerating KIC rbac helper")
			exitOnErr(updateFile(kicRBACHelperFileName, buffer))
		}
	} else {
		fmt.Println("INFO: KIC rbac helper up to date, doesn't need to be regenerated")
	}

	if failOnError {
		fmt.Println("SUCCESS: files are up to date")
	}
}

func checkout(workTree *git.Worktree, hash plumbing.Hash) {
	err := workTree.Checkout(&git.CheckoutOptions{
		Hash: hash,
		Keep: false,
	})
	if err != nil {
		fmt.Printf(
			"ERROR: failed to revert back kubernetes-ingress-controller submodule to %v: %v\n",
			hash, err,
		)
	}
	fmt.Printf("INFO: restored kubernetes-ingress-controller submodule back to %s\n", hash)
}

func generatefile(
	roles []*rbacv1.ClusterRole,
	constraint string,
	templateName string,
	template string,
	folderPath string,
	fileNamePrefix string,
) error {
	file := buildFileName(folderPath, fileNamePrefix, convertConstraintName(constraint))
	fmt.Printf("INFO: rendering file %s template for semver constraint %s\n", file, constraint)
	buffer, err := renderTemplate(roles, constraint, templateName, template)
	if err != nil {
		return err
	}
	m, err := filesEqual(file, buffer)
	if err != nil {
		return err
	}
	if !m {
		if failOnError {
			return fmt.Errorf("file %s for constraint %s out of date, please regenerate it", file, constraint)
		}
		fmt.Printf("INFO: file %s for constraint %s out of date, needs to be regenerated\n", file, constraint)
		if !dryRun {
			fmt.Printf("INFO: regenerating file %s for constraint %s\n", file, constraint)
			if err := mkdir(folderPath); err != nil {
				return err
			}
			if err := updateFile(file, buffer); err != nil {
				return err
			}
		}
	} else {
		fmt.Printf("INFO: file %s for constraint %s up to date, doesn't need to be regenerated\n", file, constraint)
	}

	return nil
}
