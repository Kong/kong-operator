package main

import (
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/Masterminds/semver"
	kongsemver "github.com/kong/semver/v4"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/kong/kong-operator/hack/generators/kic"
	kicversions "github.com/kong/kong-operator/internal/versions"
)

var clusterRoleRelativePaths = []string{
	"config/rbac/crds/role.yaml",
	"config/rbac/role.yaml",
	"config/rbac/leader_election_role.yaml",
	"config/rbac/gateway/role.yaml",
	"config/rbac/knative/role.yaml",
}

const (
	controllerRBACPath       = "./pkg/utils/kubernetes/resources/clusterroles/"
	controllerRBACFilePrefix = "zz_generated.kong_ingress_controller_rbac"

	kicRBACPath       = "./pkg/utils/kubernetes/resources/clusterroles"
	kicRBACFIlePrefix = "zz_generated.controlplane_clusterrole"

	kicRBACHelperFileName = "./pkg/utils/kubernetes/resources/zz_generated.clusterrole_helpers.go"

	docFileName = controllerRBACPath + "doc.go"
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
	if force {
		exitOnErr(rmDirs(controllerRBACPath, kicRBACPath))
		exitOnErr(mkdir(controllerRBACPath))
		exitOnErr(mkdir(kicRBACPath))

	}

	exitOnErr(renderDoc(docFileName))

	for versionConstraint, rbacVersion := range kicversions.ManifestsVersionsForKICVersions {
		fmt.Printf("INFO: checking and generating code for constraint %s with version %s\n", versionConstraint, rbacVersion)
		// ensure the version has the "v" prefix
		kicVersion := rbacVersion.String()
		if !strings.HasPrefix(kicVersion, "v") {
			kicVersion = fmt.Sprintf("v%s", kicVersion)
		}

		fmt.Printf("INFO: parsing clusterRole for KIC version %s\n", kicVersion)
		clusterRoles := []*rbacv1.ClusterRole{}
		for _, rolePath := range clusterRoleRelativePaths {
			if versionIsEqualToOrGraterThanV3(kicVersion) && rolePath == "config/rbac/knative/role.yaml" {
				continue
			}
			// Here we try to merge all the rules from all known cluster roles.
			newRole, err := getRoleFromKICRepository(rolePath, rbacVersion)
			exitOnErr(err)
			clusterRoles = append(clusterRoles, newRole)
		}

		// Don't add the same policy rules twice.
		// Those might hypothetically come from different roles which we use for generation.
		rolePermissionsCache := make(map[string]struct{}, 0)

		for _, clusterRole := range clusterRoles {
			fmt.Printf("INFO: checking clusterRole %s for KIC version %s\n", clusterRole.Name, kicVersion)

			newRules := slices.DeleteFunc(clusterRole.Rules, func(pr rbacv1.PolicyRule) bool {
				key := pr.String()
				if _, ok := rolePermissionsCache[key]; ok {
					return true
				}
				rolePermissionsCache[key] = struct{}{}
				return false
			})
			clusterRole.Rules = newRules
		}

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

	buffer, err := renderHelperTemplate(kicversions.ManifestsVersionsForKICVersions, "kic-rbac", kicRBACHelperTemplate)
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

func versionIsEqualToOrGraterThanV3(vStr string) bool {
	c, err := semver.NewConstraint(">=3.0")
	exitOnErr(err)

	v, err := semver.NewVersion(vStr)
	exitOnErr(err)

	return c.Check(v)
}

func getRoleFromKICRepository(filePath string, version kongsemver.Version) (*rbacv1.ClusterRole, error) {
	content, err := kic.GetFileFromKICRepositoryForVersion(filePath, version)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from KIC repository: %w", filePath, err)
	}

	role := &rbacv1.ClusterRole{}
	if err = yaml.Unmarshal(content, role); err != nil {
		return nil, fmt.Errorf("failed to parse role (%s) from KIC repository: %w", filePath, err)
	}

	return role, nil
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
