package v1beta1

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"
)

const (
	errWrongConvertToControlPlane   = "ControlPlane ConvertTo: expected *operatorv2beta1.ControlPlane, got %T"
	errWrongConvertFromControlPlane = "ControlPlane ConvertFrom: expected *operatorv2beta1.ControlPlane, got %T"
	errEnvVarPopulatedWithEnvFrom   = "ControlPlane v1beta1 can't be converted, because environment variable: %s is populated with EnvFrom, manual adjustment is needed"
	errEnvFromOnContainerLevel      = "ControlPlane v1beta1 can't be converted, because EnvFrom is used on container level (converter can't reason about values), manual adjustment is needed"
)

// Environment variable names for configuration.
// Based on https://developer.konghq.com/kubernetes-ingress-controller/reference/configuration-options
const (
	envControllerFeatureGates                            = "CONTROLLER_FEATURE_GATES"
	envControllerEnableReverseSync                       = "CONTROLLER_ENABLE_REVERSE_SYNC"
	envControllerGatewayDiscoveryReadinessCheckInterval  = "CONTROLLER_GATEWAY_DISCOVERY_READINESS_CHECK_INTERVAL"
	envControllerGatewayDiscoveryReadinessCheckTimeout   = "CONTROLLER_GATEWAY_DISCOVERY_READINESS_CHECK_TIMEOUT"
	envControllerK8sInitCacheSyncDuration                = "CONTROLLER_INIT_CACHE_SYNC_DURATION"
	envControllerCombinedServicesFromDifferentHTTPRoutes = "CONTROLLER_COMBINED_SERVICES_FROM_DIFFERENT_HTTPROUTES"
	envControllerUseLastValidConfigForFallback           = "CONTROLLER_USE_LAST_VALID_CONFIG_FOR_FALLBACK"
	envControllerEnableDrainSupport                      = "CONTROLLER_ENABLE_DRAIN_SUPPORT"
	envControllerEnableConfigDump                        = "CONTROLLER_DUMP_CONFIG"
	envControllerEnableConfigDumpSensitive               = "CONTROLLER_DUMP_SENSITIVE_CONFIG"
	envControllerEnableKonnectConsumersSync              = "CONTROLLER_KONNECT_DISABLE_CONSUMERS_SYNC"
	envControllerEnableKonnectLicensing                  = "CONTROLLER_KONNECT_LICENSING_ENABLED"
	envControllerKonnectInitialLicensePollingPeriod      = "CONTROLLER_KONNECT_INITIAL_LICENSE_POLLING_PERIOD"
	envControllerKonnectPollingPeriod                    = "CONTROLLER_KONNECT_LICENSE_POLLING_PERIOD"
	envControllerEnableKonnectLicensingStorage           = "CONTROLLER_KONNECT_LICENSE_STORAGE_ENABLED"
	envControllerKonnectNodeRefreshPeriod                = "CONTROLLER_KONNECT_REFRESH_NODE_PERIOD"
	envControllerKonnectConfigUploadPeriod               = "CONTROLLER_KONNECT_UPLOAD_CONFIG_PERIOD"
	envControllerIngressClass                            = "CONTROLLER_INGRESS_CLASS"

	// Environment variable prefix for controller enable/disable. After the prefix is here the name of a controller.
	// It matches format of what is in the new ControlPlane.
	envControllerPrefix = "CONTROLLER_ENABLE_CONTROLLER_"

	// Environment variable that maps to boolean values.
	envValueTrue  = "true"
	envValueFalse = "false"
	envValueOne   = "1"
	envValueZero  = "0"

	// State values.
	stateEnabled  = "enabled"
	stateDisabled = "disabled"
)

// ConvertTo converts from this version (v1beta1) to the Hub version.
func (c *ControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*operatorv2beta1.ControlPlane)
	if !ok {
		return fmt.Errorf(errWrongConvertToControlPlane, dstRaw)
	}

	dst.ObjectMeta = c.ObjectMeta

	if err := c.Spec.convertTo(&dst.Spec.ControlPlaneOptions, c.Spec.IngressClass); err != nil {
		return err
	}
	dst.Spec.Extensions = c.Spec.Extensions

	if dp := lo.FromPtr(c.Spec.DataPlane); dp != "" {
		dst.Spec.DataPlane = operatorv2beta1.ControlPlaneDataPlaneTarget{
			Type: operatorv2beta1.ControlPlaneDataPlaneTargetRefType,
			Ref: &operatorv2beta1.ControlPlaneDataPlaneTargetRef{
				Name: dp,
			},
		}
	} else {
		dst.Spec.DataPlane = operatorv2beta1.ControlPlaneDataPlaneTarget{
			Type: operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType,
		}
	}

	dst.Status = operatorv2beta1.ControlPlaneStatus{
		Conditions: c.Status.Conditions,
	}

	return nil
}

// ConvertFrom converts from the Hub version to this version (v1beta1).
func (c *ControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*operatorv2beta1.ControlPlane)
	if !ok {
		return fmt.Errorf(errWrongConvertFromControlPlane, srcRaw)
	}

	c.ObjectMeta = src.ObjectMeta

	c.Spec.IngressClass = src.Spec.IngressClass
	c.Spec.Extensions = src.Spec.Extensions
	if src.Spec.DataPlane.Type == operatorv2beta1.ControlPlaneDataPlaneTargetRefType && src.Spec.DataPlane.Ref != nil {
		c.Spec.DataPlane = &src.Spec.DataPlane.Ref.Name
	}

	c.Spec.convertFrom(src.Spec.ControlPlaneOptions)

	c.Status = ControlPlaneStatus{
		Conditions: src.Status.Conditions,
	}

	return nil
}

func (c *ControlPlaneOptions) convertTo(dst *operatorv2beta1.ControlPlaneOptions, ingressClass *string) error {
	if c == nil {
		return nil
	}
	var containerEnvVars []corev1.EnvVar
	if pts := c.Deployment.PodTemplateSpec; pts != nil && len(pts.Spec.Containers) > 0 {
		if pts.Spec.Containers[0].EnvFrom != nil {
			return errors.New(errEnvFromOnContainerLevel)
		}
		containerEnvVars = pts.Spec.Containers[0].Env
	}

	fgs, err := featureGatesFromEnvVar(containerEnvVars)
	if err != nil {
		return err
	}
	ctrls, err := cpControllersFormatFromEnvVars(containerEnvVars)
	if err != nil {
		return err
	}

	nn := lo.FromPtr(c.WatchNamespaces)
	reverseSync, err := parseEnvForToggle[operatorv2beta1.ControlPlaneReverseSyncState](envControllerEnableReverseSync, containerEnvVars)
	if err != nil {
		return err
	}

	readinessCheckInterval, err := parseEnvForDuration(envControllerGatewayDiscoveryReadinessCheckInterval, containerEnvVars)
	if err != nil {
		return err
	}

	readinessCheckTimeout, err := parseEnvForDuration(envControllerGatewayDiscoveryReadinessCheckTimeout, containerEnvVars)
	if err != nil {
		return err
	}

	initSyncDuration, err := parseEnvForDuration(envControllerK8sInitCacheSyncDuration, containerEnvVars)
	if err != nil {
		return err
	}

	combinedServicesFromDifferentHTTPRoutes, err := parseEnvForToggle[operatorv2beta1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesState](envControllerCombinedServicesFromDifferentHTTPRoutes, containerEnvVars)
	if err != nil {
		return err
	}

	drainSupport, err := parseEnvForToggle[operatorv2beta1.ControlPlaneDrainSupportState](envControllerEnableDrainSupport, containerEnvVars)
	if err != nil {
		return err
	}

	// Handle FallbackConfiguration parsing
	lastCfg, err := parseEnvForToggle[operatorv2beta1.ControlPlaneFallbackConfigurationState](envControllerUseLastValidConfigForFallback, containerEnvVars)
	if err != nil {
		return err
	}
	var fallbackConfig *operatorv2beta1.ControlPlaneFallbackConfiguration
	if lastCfg != nil {
		fallbackConfig = &operatorv2beta1.ControlPlaneFallbackConfiguration{
			UseLastValidConfig: lastCfg,
		}
	}

	consumersSync, err := parseEnvForToggle[operatorv2beta1.ControlPlaneKonnectConsumersSyncState](envControllerEnableKonnectConsumersSync, containerEnvVars)
	if err != nil {
		return err
	}

	// Handle Konnect Licensing parsing
	licensingState, err := parseEnvForToggle[operatorv2beta1.ControlPlaneKonnectLicensingState](envControllerEnableKonnectLicensing, containerEnvVars)
	if err != nil {
		return err
	}
	var licensing *operatorv2beta1.ControlPlaneKonnectLicensing
	if licensingState != nil && *licensingState != operatorv2beta1.ControlPlaneKonnectLicensingStateDisabled {
		initialPollingPeriod, err := parseEnvForDuration(envControllerKonnectInitialLicensePollingPeriod, containerEnvVars)
		if err != nil {
			return err
		}

		pollingPeriod, err := parseEnvForDuration(envControllerKonnectPollingPeriod, containerEnvVars)
		if err != nil {
			return err
		}

		storageState, err := parseEnvForToggle[operatorv2beta1.ControlPlaneKonnectLicensingState](envControllerEnableKonnectLicensingStorage, containerEnvVars)
		if err != nil {
			return err
		}

		licensing = &operatorv2beta1.ControlPlaneKonnectLicensing{
			State:                licensingState,
			InitialPollingPeriod: initialPollingPeriod,
			PollingPeriod:        pollingPeriod,
			StorageState:         storageState,
		}
	}

	nodeRefreshPeriod, err := parseEnvForDuration(envControllerKonnectNodeRefreshPeriod, containerEnvVars)
	if err != nil {
		return err
	}

	configUploadPeriod, err := parseEnvForDuration(envControllerKonnectConfigUploadPeriod, containerEnvVars)
	if err != nil {
		return err
	}

	*dst = operatorv2beta1.ControlPlaneOptions{
		IngressClass: ingressClass,

		WatchNamespaces: lo.ToPtr(operatorv2beta1.WatchNamespaces{
			Type: operatorv2beta1.WatchNamespacesType(nn.Type),
			List: nn.List,
		}),
		FeatureGates: fgs,
		Controllers:  ctrls,
		DataPlaneSync: &operatorv2beta1.ControlPlaneDataPlaneSync{
			ReverseSync: reverseSync,
		},
		GatewayDiscovery: &operatorv2beta1.ControlPlaneGatewayDiscovery{
			ReadinessCheckInterval: readinessCheckInterval,
			ReadinessCheckTimeout:  readinessCheckTimeout,
		},
		Cache: &operatorv2beta1.ControlPlaneK8sCache{
			InitSyncDuration: initSyncDuration,
		},
		Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
			CombinedServicesFromDifferentHTTPRoutes: combinedServicesFromDifferentHTTPRoutes,
			FallbackConfiguration:                   fallbackConfig,
			DrainSupport:                            drainSupport,
		},
		ConfigDump: &operatorv2beta1.ControlPlaneConfigDump{
			State:         getConfigDumpState(envControllerEnableConfigDump, containerEnvVars),
			DumpSensitive: getConfigDumpState(envControllerEnableConfigDumpSensitive, containerEnvVars),
		},
		Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
			ConsumersSync:      consumersSync,
			Licensing:          licensing,
			NodeRefreshPeriod:  nodeRefreshPeriod,
			ConfigUploadPeriod: configUploadPeriod,
		},
	}
	return nil
}

func (c *ControlPlaneOptions) convertFrom(src operatorv2beta1.ControlPlaneOptions, opts ...func(*[]corev1.EnvVar)) {
	if c == nil {
		return
	}
	if src.WatchNamespaces != nil {
		c.WatchNamespaces = lo.ToPtr(WatchNamespaces{
			Type: WatchNamespacesType(src.WatchNamespaces.Type),
			List: src.WatchNamespaces.List,
		})
	}
	c.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "controller",
					Env:  buildContainerEnvVars(src),
				},
			},
		},
	}

	for _, opt := range opts {
		opt(&c.Deployment.PodTemplateSpec.Spec.Containers[0].Env)
	}
}

func featureGatesFromEnvVar(envs []corev1.EnvVar) ([]operatorv2beta1.ControlPlaneFeatureGate, error) {
	fgEnvVar, ok := lo.Find(envs, func(fg corev1.EnvVar) bool {
		return fg.Name == envControllerFeatureGates
	})
	if !ok {
		return nil, nil
	}
	if fgEnvVar.ValueFrom != nil {
		return nil, fmt.Errorf(errEnvVarPopulatedWithEnvFrom, fgEnvVar.Name)
	}

	fgKeyValues := strings.Split(fgEnvVar.Value, ",")
	featureGates := make([]operatorv2beta1.ControlPlaneFeatureGate, 0, len(fgKeyValues))
	for _, fgKeyValue := range fgKeyValues {
		key, value, err := parseKeyValue(fgKeyValue)
		if err != nil {
			return nil, fmt.Errorf("failed to parse feature gate, %w", err)
		}

		var fgState operatorv2beta1.FeatureGateState
		switch value {
		case envValueTrue:
			fgState = operatorv2beta1.FeatureGateStateEnabled
		case envValueFalse:
			fgState = operatorv2beta1.FeatureGateStateDisabled
		default:
			return nil, fmt.Errorf("invalid value for feature gate %s, expected 'true' or 'false' as value", fgKeyValue)
		}
		featureGates = append(featureGates, operatorv2beta1.ControlPlaneFeatureGate{
			Name:  key,
			State: fgState,
		})
	}
	return featureGates, nil
}

func envVarFromFeatureGates(featureGates []operatorv2beta1.ControlPlaneFeatureGate) []corev1.EnvVar {
	if len(featureGates) == 0 {
		return nil
	}

	fgPairs := make([]string, 0, len(featureGates))
	for _, fg := range featureGates {
		var value string
		switch fg.State {
		case operatorv2beta1.FeatureGateStateEnabled:
			value = envValueTrue
		case operatorv2beta1.FeatureGateStateDisabled:
			value = envValueFalse
		default:
			// Skip invalid states.
			continue
		}
		fgPairs = append(fgPairs, fmt.Sprintf("%s=%s", fg.Name, value))
	}

	if len(fgPairs) == 0 {
		return nil
	}

	return []corev1.EnvVar{
		{
			Name:  envControllerFeatureGates,
			Value: strings.Join(fgPairs, ","),
		},
	}
}

func envVarsFromCPControllersFormat(controllers []operatorv2beta1.ControlPlaneController) []corev1.EnvVar {
	if len(controllers) == 0 {
		return nil
	}

	envVars := make([]corev1.EnvVar, 0, len(controllers))
	for _, ctrl := range controllers {
		var value string
		switch ctrl.State {
		case operatorv2beta1.ControllerStateEnabled:
			value = envValueTrue
		case operatorv2beta1.ControllerStateDisabled:
			value = envValueFalse
		default:
			// Skip invalid states.
			continue
		}

		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerPrefix + ctrl.Name,
			Value: value,
		})
	}

	return envVars
}

func ingressClassFormatFromEnvVars(envs []corev1.EnvVar) *string {
	ingressClassEnv, found := lo.Find(envs, func(env corev1.EnvVar) bool {
		return env.Name == envControllerIngressClass
	})
	if !found {
		return nil
	}
	return lo.ToPtr(ingressClassEnv.Value)
}

func cpControllersFormatFromEnvVars(envs []corev1.EnvVar) ([]operatorv2beta1.ControlPlaneController, error) {
	controllersEnvs := lo.Filter(envs, func(env corev1.EnvVar, _ int) bool {
		return strings.HasPrefix(env.Name, envControllerPrefix)
	})
	var ctrls []operatorv2beta1.ControlPlaneController
	for _, ctrlEnv := range controllersEnvs {
		if ctrlEnv.ValueFrom != nil {
			return nil, fmt.Errorf(errEnvVarPopulatedWithEnvFrom, ctrlEnv.Name)
		}
		ctrlName := strings.TrimPrefix(ctrlEnv.Name, envControllerPrefix)
		var ctrlState operatorv2beta1.ControllerState
		switch strings.ToLower(strings.TrimSpace(ctrlEnv.Value)) {
		case envValueTrue:
			ctrlState = operatorv2beta1.ControllerStateEnabled
		case envValueFalse:
			ctrlState = operatorv2beta1.ControllerStateDisabled
		default:
			return nil, fmt.Errorf("invalid value for controller %s, expected 'true' or 'false' as value", ctrlEnv.Name)
		}

		ctrls = append(ctrls, operatorv2beta1.ControlPlaneController{
			Name:  ctrlName,
			State: ctrlState,
		})
	}

	return ctrls, nil
}

func parseKeyValue(keyValue string) (key string, value string, err error) {
	parts := strings.Split(keyValue, "=")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid key-value pair %q, expected 'key=value' format", keyValue)
	}
	key = strings.TrimSpace(parts[0])
	value = strings.ToLower(strings.TrimSpace(parts[1]))
	if key == "" || value == "" {
		return "", "", fmt.Errorf("invalid key-value pair %q, expected 'key=value' format", keyValue)
	}
	return key, value, nil
}

func parseEnvForToggle[T ~string](key string, envVars []corev1.EnvVar) (value *T, err error) {
	v, ok := lo.Find(envVars, func(env corev1.EnvVar) bool {
		return env.Name == key
	})
	if !ok {
		return nil, nil
	}
	if v.ValueFrom != nil {
		return nil, fmt.Errorf(errEnvVarPopulatedWithEnvFrom, v.Name)
	}
	switch strings.ToLower(v.Value) {
	case envValueTrue, envValueOne:
		return lo.ToPtr(T(stateEnabled)), nil
	case envValueFalse, envValueZero:
		return lo.ToPtr(T(stateDisabled)), nil
	}
	return nil, nil
}

func parseEnvForDuration(key string, envVars []corev1.EnvVar) (*metav1.Duration, error) {
	v, ok := lo.Find(envVars, func(env corev1.EnvVar) bool {
		return env.Name == key
	})
	if !ok {
		return nil, nil
	}
	if v.ValueFrom != nil {
		return nil, fmt.Errorf(errEnvVarPopulatedWithEnvFrom, v.Name)
	}
	if v.Value == "" {
		return nil, nil
	}
	d, err := time.ParseDuration(v.Value)
	if err != nil {
		return nil, err
	}
	return &metav1.Duration{
		Duration: d,
	}, nil
}

// buildContainerEnvVars builds the complete set of environment variables from ControlPlaneOptions.
func buildContainerEnvVars(cpOpts operatorv2beta1.ControlPlaneOptions, opts ...func([]corev1.EnvVar) []corev1.EnvVar) []corev1.EnvVar {
	var envVars []corev1.EnvVar

	envVars = append(envVars, envVarFromFeatureGates(cpOpts.FeatureGates)...)
	envVars = append(envVars, envVarsFromCPControllersFormat(cpOpts.Controllers)...)

	envVars = append(envVars, envVarFromDataPlaneSync(cpOpts.DataPlaneSync)...)
	envVars = append(envVars, envVarFromGatewayDiscovery(cpOpts.GatewayDiscovery)...)
	envVars = append(envVars, envVarFromCache(cpOpts.Cache)...)
	envVars = append(envVars, envVarFromTranslation(cpOpts.Translation)...)
	envVars = append(envVars, envVarFromConfigDump(cpOpts.ConfigDump)...)
	envVars = append(envVars, envVarFromKonnect(cpOpts.Konnect)...)

	for _, opt := range opts {
		envVars = opt(envVars)
	}

	return envVars
}

// envVarFromIngressClass converts IngressClass options to environment variables.
func envVarFromIngressClass(ingressClass *string) []corev1.EnvVar {
	if ingressClass == nil {
		return nil
	}

	return []corev1.EnvVar{
		{
			Name:  envControllerIngressClass,
			Value: *ingressClass,
		},
	}
}

// envVarFromDataPlaneSync converts DataPlaneSync options to environment variables.
func envVarFromDataPlaneSync(dps *operatorv2beta1.ControlPlaneDataPlaneSync) []corev1.EnvVar {
	if dps == nil {
		return nil
	}

	var envVars []corev1.EnvVar
	if dps.ReverseSync != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerEnableReverseSync,
			Value: toggleToEnvValue(*dps.ReverseSync),
		})
	}
	return envVars
}

// envVarFromGatewayDiscovery converts GatewayDiscovery options to environment variables.
func envVarFromGatewayDiscovery(gd *operatorv2beta1.ControlPlaneGatewayDiscovery) []corev1.EnvVar {
	if gd == nil {
		return nil
	}

	var envVars []corev1.EnvVar
	if gd.ReadinessCheckInterval != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerGatewayDiscoveryReadinessCheckInterval,
			Value: gd.ReadinessCheckInterval.Duration.String(),
		})
	}
	if gd.ReadinessCheckTimeout != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerGatewayDiscoveryReadinessCheckTimeout,
			Value: gd.ReadinessCheckTimeout.Duration.String(),
		})
	}
	return envVars
}

// envVarFromCache converts Cache options to environment variables.
func envVarFromCache(cache *operatorv2beta1.ControlPlaneK8sCache) []corev1.EnvVar {
	if cache == nil {
		return nil
	}

	var envVars []corev1.EnvVar
	if cache.InitSyncDuration != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerK8sInitCacheSyncDuration,
			Value: cache.InitSyncDuration.Duration.String(),
		})
	}
	return envVars
}

// envVarFromTranslation converts Translation options to environment variables.
func envVarFromTranslation(trans *operatorv2beta1.ControlPlaneTranslationOptions) []corev1.EnvVar {
	if trans == nil {
		return nil
	}

	var envVars []corev1.EnvVar
	if trans.CombinedServicesFromDifferentHTTPRoutes != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerCombinedServicesFromDifferentHTTPRoutes,
			Value: toggleToEnvValue(*trans.CombinedServicesFromDifferentHTTPRoutes),
		})
	}
	if trans.FallbackConfiguration != nil && trans.FallbackConfiguration.UseLastValidConfig != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerUseLastValidConfigForFallback,
			Value: toggleToEnvValue(*trans.FallbackConfiguration.UseLastValidConfig),
		})
	}
	if trans.DrainSupport != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerEnableDrainSupport,
			Value: toggleToEnvValue(*trans.DrainSupport),
		})
	}
	return envVars
}

// envVarFromConfigDump converts ConfigDump options to environment variables.
func envVarFromConfigDump(cd *operatorv2beta1.ControlPlaneConfigDump) []corev1.EnvVar {
	if cd == nil {
		return nil
	}

	var envVars []corev1.EnvVar
	envVars = append(envVars, corev1.EnvVar{
		Name:  envControllerEnableConfigDump,
		Value: toggleToEnvValue(cd.State),
	})
	envVars = append(envVars, corev1.EnvVar{
		Name:  envControllerEnableConfigDumpSensitive,
		Value: toggleToEnvValue(cd.DumpSensitive),
	})
	return envVars
}

// envVarFromKonnect converts Konnect options to environment variables.
func envVarFromKonnect(konnect *operatorv2beta1.ControlPlaneKonnectOptions) []corev1.EnvVar {
	if konnect == nil {
		return nil
	}

	var envVars []corev1.EnvVar
	if konnect.ConsumersSync != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerEnableKonnectConsumersSync,
			Value: toggleToEnvValue(*konnect.ConsumersSync),
		})
	}
	if konnect.Licensing != nil {
		if konnect.Licensing.State != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name:  envControllerEnableKonnectLicensing,
				Value: toggleToEnvValue(*konnect.Licensing.State),
			})
		}
		if konnect.Licensing.InitialPollingPeriod != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name:  envControllerKonnectInitialLicensePollingPeriod,
				Value: konnect.Licensing.InitialPollingPeriod.Duration.String(),
			})
		}
		if konnect.Licensing.PollingPeriod != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name:  envControllerKonnectPollingPeriod,
				Value: konnect.Licensing.PollingPeriod.Duration.String(),
			})
		}
		if konnect.Licensing.StorageState != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name:  envControllerEnableKonnectLicensingStorage,
				Value: toggleToEnvValue(*konnect.Licensing.StorageState),
			})
		}
	}
	if konnect.NodeRefreshPeriod != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerKonnectNodeRefreshPeriod,
			Value: konnect.NodeRefreshPeriod.Duration.String(),
		})
	}
	if konnect.ConfigUploadPeriod != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  envControllerKonnectConfigUploadPeriod,
			Value: konnect.ConfigUploadPeriod.Duration.String(),
		})
	}
	return envVars
}

// toggleToEnvValue converts a toggle state to environment variable value.
func toggleToEnvValue[T ~string](state T) string {
	switch strings.ToLower(string(state)) {
	case stateEnabled:
		return envValueTrue
	case stateDisabled:
		return envValueFalse
	default:
		return envValueFalse
	}
}

// getConfigDumpState safely gets ConfigDumpState from environment variables.
func getConfigDumpState(key string, envVars []corev1.EnvVar) operatorv2beta1.ConfigDumpState {
	state, err := parseEnvForToggle[operatorv2beta1.ConfigDumpState](key, envVars)
	if err != nil || state == nil {
		return operatorv2beta1.ConfigDumpStateDisabled
	}
	return *state
}
