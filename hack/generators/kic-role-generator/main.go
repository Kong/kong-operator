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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	exitOnErr(err, "failed opening '.'")

	gatewayWorktree, err := gatewayRepo.Worktree()
	exitOnErr(err, "failed getting gateway operator's work tree")

	kicSubmodule, err := gatewayWorktree.Submodule("kubernetes-ingress-controller")
	exitOnErr(err, "failed getting KIC's submodule")
	kicStatus, err := kicSubmodule.Status()
	exitOnErr(err, "failed getting KIC's submodule status")
	if !kicStatus.IsClean() {
		exitOnErr(
			fmt.Errorf("status of kubernetes-ingress-controller submodule is not clean: %s", kicStatus))
	}
	prevHead := kicStatus.Current

	err = kicSubmodule.Init()
	if err != nil && !errors.Is(err, git.ErrSubmoduleAlreadyInitialized) {
		exitOnErr(err, "failed initializing KIC's submodule")
	}

	err = kicSubmodule.UpdateContext(context.Background(),
		&git.SubmoduleUpdateOptions{
			Init:    false,
			NoFetch: true,
		},
	)
	exitOnErr(err, "failed updating KIC's submodule")

	kicRepo, err := kicSubmodule.Repository()
	exitOnErr(err, "failed getting KIC's repository")

	err = kicRepo.FetchContext(context.Background(), &git.FetchOptions{})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		exitOnErr(err, "failed fetching for KIC's submodule repo")
	}

	kicWorktree, err := kicRepo.Worktree()
	exitOnErr(err, "failed getting KIC's work tree")

	if force {
		exitOnErr(rmDirs(controllerRBACPath, kicRBACPath))
	}

	// defer reverting KIC's submodule back to status from before.
	defer checkout(kicWorktree, prevHead)
	for versionConstraint, rbacVersion := range kicversions.RoleVersionsForKICVersions {
		fmt.Printf("INFO: checking and generating code for constraint %s with version %s\n", versionConstraint, rbacVersion)
		// ensure the version has the "v" prefix
		version := semver.MustParse(rbacVersion).String()
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

		// Don't add the same policy rules twice.
		// Those might hypothetically come from different roles which we use for generation.
		rolePermissionsCache := make(map[string]struct{}, 0)
		for _, clusterRole := range clusterRoles {
			for idx, policyRule := range clusterRole.Rules {
				key := policyRule.String()
				if _, ok := rolePermissionsCache[key]; ok {
					clusterRole.Rules = append(clusterRole.Rules[:idx], clusterRole.Rules[idx+1:]...)
					continue
				}
				rolePermissionsCache[key] = struct{}{}
			}
		}
		// TODO: Instead of adding a broken config/rbac/leader_election_role.yaml
		// role from KIC - which was fixed in
		// https://github.com/Kong/kubernetes-ingress-controller/pull/3932
		// but yet unreleased - we manually add the leader election policy rules
		// to allow KIC to use them for leader election.
		//
		// Ref: https://github.com/Kong/gateway-operator/issues/744
		clusterRoles = append(clusterRoles, &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: "leader-election-stopgap-cluster-role",
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
				},
				{
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
					APIGroups: []string{"coordination.k8s.io"},
					Resources: []string{"leases"},
				},
			},
		})

		exitOnErr(generatefile(
			clusterRoles,
			versionConstraint,
			"kic-rbac",
			kicRBACTemplate,
			kicRBACPath,
			kicRBACFIlePrefix,
		))

		exitOnErr(generatefile(
			clusterRoles,
			versionConstraint,
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
	versionConstraint string,
	templateName string,
	template string,
	folderPath string,
	fileNamePrefix string,
) error {
	file := buildFileName(folderPath, fileNamePrefix, convertConstraintName(versionConstraint))
	fmt.Printf("INFO: rendering file %s template for semver constraint %s\n", file, versionConstraint)
	buffer, err := renderTemplate(roles, versionConstraint, templateName, template)
	if err != nil {
		return err
	}
	m, err := filesEqual(file, buffer)
	if err != nil {
		return err
	}
	if !m {
		if failOnError {
			return fmt.Errorf("file %s for constraint %s out of date, please regenerate it", file, versionConstraint)
		}
		fmt.Printf("INFO: file %s for constraint %s out of date, needs to be regenerated\n", file, versionConstraint)
		if !dryRun {
			fmt.Printf("INFO: regenerating file %s for constraint %s\n", file, versionConstraint)
			if err := mkdir(folderPath); err != nil {
				return err
			}
			if err := updateFile(file, buffer); err != nil {
				return err
			}
		}
	} else {
		fmt.Printf("INFO: file %s for constraint %s up to date, doesn't need to be regenerated\n", file, versionConstraint)
	}

	return nil
}
