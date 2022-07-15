package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/go-git/go-git/v5"
	rbacv1 "k8s.io/api/rbac/v1"

	kicversions "github.com/kong/gateway-operator/internal/versions"
)

const (
	gitClonePath            = "./kubernetes-ingress-controller"
	clusterRoleRelativePath = "config/rbac/role.yaml"

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
	defer cleanup()

	// clone KIC Github repository and extract worktree
	repo, err := git.PlainClone(gitClonePath, false, &git.CloneOptions{
		URL:      "https://github.com/kong/kubernetes-ingress-controller",
		Progress: os.Stdout,
	})
	exitOnErr(err)
	workTree, err := repo.Worktree()
	exitOnErr(err)

	if force {
		exitOnErr(rmDirs(controllerRBACPath, kicRBACPath))
	}

	for c, v := range kicversions.RoleVersionsForKICVersions {
		fmt.Printf("INFO: checking and generating code for constraint %s with version %s\n", c, v)
		// ensure the version has the "v" prefix
		version := semver.MustParse(v).String()
		if !strings.HasPrefix(version, "v") {
			version = fmt.Sprintf("v%s", version)
		}
		fmt.Printf("INFO: checking out tag %s\n", version)
		exitOnErr(gitCheckoutTag(repo, workTree, version))

		fmt.Printf("INFO: parsing clusterRole for KIC version %s\n", version)
		rolePath := path.Join(gitClonePath, clusterRoleRelativePath)
		newRole, err := parseRole(rolePath)
		exitOnErr(err)

		exitOnErr(generatefile(
			newRole,
			c,
			"kic-rbac",
			kicRBACTemplate,
			kicRBACPath,
			kicRBACFIlePrefix,
		))

		exitOnErr(generatefile(
			newRole,
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

func generatefile(role *rbacv1.ClusterRole,
	constraint string,
	templateName string,
	template string,
	folderPath string,
	fileNamePrefix string,
) error {
	file := buildFileName(folderPath, fileNamePrefix, convertConstraintName(constraint))
	fmt.Printf("INFO: rendering file %s template for semver constraint %s\n", file, constraint)
	buffer, err := renderTemplate(role, constraint, templateName, template)
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
