package configuration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

const (
	// caBundleOf7 holds 7 certificates, over 7 KB.
	caBundleOf7 = "-----BEGIN CERTIFICATE-----\nMIIC7DCCAdSgAwIBAgIUQYoiGYzuXPu8VQ2xXBE5E5tcaJQwDQYJKoZIhvcNAQEL\nBQAwFjEUMBIGA1UEAwwLY3JkdmFsLWNhLTEwHhcNMjYxMTI1MDkxNDMxWhcNMjYx\nMTI3MDkxNDMxWjAWMRQwEgYDVQQDDAtjcmR2YWwtY2EtMTCCASIwDQYJKoZIhvcN\nAQEBBQADggEPADCCAQoCggEBAKRqRHpbzJhiINPgP4iJqWg/1v01iI4USx2Oa4Yu\newmqQouDApKkitr9HuAdzh8X4Ou/3vicBsqCC+Xy2MQz9txzPRqEF0KTGS9QM4X4\n2DOg24Mm4xMcfoniyIESAz600UdSOljkpE974jzkowfS0vmSgb3vv6MUM8H9pj8W\nBxg1t7IVuZG1t9+t3IdBDueVxs+4mUeqpBx6ekdTGZqpTAPseQJ6CLqNhfGzTdcr\n29KV5eGt7u5Kr9DKfJlTc+LZVk2M9Chs4CaeDn4IiO9rlHAOk5aeNrB1jVWtVV3t\nhU6M3WlEd+P/83yFtpq19VAouK6b70it4cqXtHmriwIpCFkCAwEAAaMyMDAwHQYD\nVR0OBBYEFHsPwxMT0uUEkac4JY+YWi/kYntTMA8GA1UdEwEB/wQFMAMBAf8wDQYJ\nKoZIhvcNAQELBQADggEBAJlKhy6qjKQ4zzimilU5ehQrJbxXxLOb3lrLrcjlEHo5\nxvN5mee0hJG4HXasm1ry2ICoGHf2wxg6u2ssnz0uM49sZumfPS3ZkDBlGV7e4t7U\nh835pzAzS0d+/4NaF7/zieeMovd9fM/IXdBUq4XLDWh+iS5b2pja7W1nLntg5V9y\nvOIyJhiAyf7bAr6p3TSuLPppGA72x6U3P9dU9gquhqVl8E4YWxtsCz62UI70VAfO\nVc2h1Dh4HrT7GcVTzB0go9LqAetxvBHr2ZGo308VMHJg1cvIe4pGX9neYAQO/R/F\n8Kl3ERAPjCjpegffgjQLQrPnAfANw5SV2HTsHiNqmHk=\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIC7DCCAdSgAwIBAgIUZqIEqt7qHaERgaOOK6XgFG8aWFkwDQYJKoZIhvcNAQEL\nBQAwFjEUMBIGA1UEAwwLY3JkdmFsLWNhLTIwHhcNMjYxMTI1MDkxNDMxWhcNMjYx\nMTI3MDkxNDMxWjAWMRQwEgYDVQQDDAtjcmR2YWwtY2EtMjCCASIwDQYJKoZIhvcN\nAQEBBQADggEPADCCAQoCggEBAL4qKWO5pT37EoS2L1VEzKPj8fMWIOh4ygwjk8FG\nDNWcxggcvZGsy2QFy0d/LRfAj9FKl8Vg/NcqG/ChUn3oAY5QJn3dwPB1g6zVDJnh\n+ZFjd+TgedhlG7vpiu/QHkPl75fg10Bpt2bLMho6utWe3bU74I+hCspToGaRH46o\nebP28PycxzHfncmGTE5z54PDRhM4ZspcmkjWegECdu6MhSmOo78n6yiOXqWbXbGS\n9gI1LgfQl/w9klVrOmfx+F4nog6NzP62ltPGN2nmLEVt0wrxKF3xLbCJPI4GD27m\nmoKR1B4oVBpGUa1TgtgMeojxZ5AUUsbskCD8gZm2QJB+LdcCAwEAAaMyMDAwHQYD\nVR0OBBYEFOjIAh5KVL9zRq+jodqK/Ep2248TMA8GA1UdEwEB/wQFMAMBAf8wDQYJ\nKoZIhvcNAQELBQADggEBAG3yQmc18hXxkjfxtos2Bqlk8VAf2sptlRVmexzM/yYs\nFhDlk+Sj6LE394WE2n8ZyyHu7BquPTb00WfmFsB1C227hnndi67wiW4/+2r47ti6\nho3uluF/2SmrAYFCmfz1XTib0cgdEA/FQBQ03fiADu7zrVQx1dcDpgNgwW7167y/\n/2Qes87/UAqzsj9dveWM/UJMZnLh1hDiiR7QmUVTWshgxxFCe94CzQDKMosjd0H/\ndEV1wGc5kMKMBSBdUYjGJhZoNKy49XJ0G8lH0zNr6zLC8fTlxp09IXX2sHnLpLQU\nXyu3bLMbbJce3gpdt1R3hMWiTZ0UEPSe1q9zAByh428=\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIC7DCCAdSgAwIBAgIUGHTmdp4u7uPEKM2f8Xlw2oIgYsgwDQYJKoZIhvcNAQEL\nBQAwFjEUMBIGA1UEAwwLY3JkdmFsLWNhLTMwHhcNMjYxMTI1MDkxNDMxWhcNMjYx\nMTI3MDkxNDMxWjAWMRQwEgYDVQQDDAtjcmR2YWwtY2EtMzCCASIwDQYJKoZIhvcN\nAQEBBQADggEPADCCAQoCggEBALX0PF1owqNYDWeIU5sPtrkaEGiNawny2Q1QVqcL\nx7jIZeC/sF+cbw1gRp80IGINUx1FH7PIG4H4wbac0iIqRT8HMhhVm7q7j4Z/HrdD\nuTBRHYMMIYcGv+nZZCnznc4haxjrl5wdyB4SRS7pKwOvNOi02r+WmrOuxfVn0FRl\n3sWWn70Op3F5RuU5K2e/0O0v9RCJ0oOrs9ge5pVTdgjsc4B5U6qlR5w/eE7QHMtE\n/i/NrYBFseigBSo+OhsYl9uuRYlDZRp+bIEHIFsDo70iul0p6+/Hyj5RAbpWK5VB\nXgvs3mwRh82uxLCCGG0zdJnALvNyCGqn268x8VS2ymDBLpkCAwEAAaMyMDAwHQYD\nVR0OBBYEFGClU9YcnZjPLx+6/mVDKmSjh4iBMA8GA1UdEwEB/wQFMAMBAf8wDQYJ\nKoZIhvcNAQELBQADggEBAJqEZhbQiUJFmFXkfNgy4vdR0c4bMaHTdWLyeBafCDt1\npDg7txJe11HCJEqvFoPlxii6n9ktwucksmbPwm/5PPeDzqqXcIgHnCzFVrX8qhwy\nQkw8+3+ANtUFlZ8gju6N9vT8v4QVpoAvqCj8bS4eg5F/PDtOEXWUPqP55mmmLWuI\n33H7tZDRoinkvEKuVP8XXkveM9HIVdHQi/pHNu+nq/fABKeEKXIyo7uRpUbLm7zQ\n6ArmY79lB+gAk2vaJb2oy0dnKtqDK6mzF3PK210v0EAPUjF7Tf7cswYdwx64CJgs\nJ2lVtu08MDaTaCH3KfZgiUL0ngkMWyAqXU6PSgzVAqM=\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIC7DCCAdSgAwIBAgIUVgSGYCVRgGVaIZ9vCvX+lJqsqvkwDQYJKoZIhvcNAQEL\nBQAwFjEUMBIGA1UEAwwLY3JkdmFsLWNhLTQwHhcNMjYxMTI1MDkxNDMxWhcNMjYx\nMTI3MDkxNDMxWjAWMRQwEgYDVQQDDAtjcmR2YWwtY2EtNDCCASIwDQYJKoZIhvcN\nAQEBBQADggEPADCCAQoCggEBANf1DXUrsaUoUutRa4j44T7jMGJ/7NN7r8iIkt+F\nVEvekNutIPvVLU2N3tgdYK7VG/XGtGInQ4Kk8I5OFI0WfNBxbtIl3RhOS9fpNSmm\nfzFHj4XRPzkKb6iSiyl4Oqf+DEm8e/O2k5GuWe8eWOceKjGkXvAqfISfwscmaKwM\n83y404kIJEWVSt7jmvNXxMutuszfiisLmZxshpLKrz4PNs7eQuaegdAPqNqXTRF7\nT8Hv4rkRhtSLY8ZHlBRMrI8AyCi8MKScUSgakSZLIF2AGSX4LF3YhXUeKREu/w1b\ncYvhHXga5CSrfeV6jEXlQs0nc5Uql4YENWGm0oNuI0sjfa8CAwEAAaMyMDAwHQYD\nVR0OBBYEFMi+x+Oz1v6k2nWzHnD/171GLZENMA8GA1UdEwEB/wQFMAMBAf8wDQYJ\nKoZIhvcNAQELBQADggEBAC3SDNl3E5TgClTxJHVZsgI5l5bczULaAhCZAc70HvUB\nlCnFOxqttFbXYhpRUo05T4IZG0CJuYggeaORSU/4DlPs2atHCjM3bzNRpcYdX7uK\nBACwy43dxzUObSmdXdZaSpGHsNYPcE/SZDo7wQzwXabSkV3DsLvnpTfpLnPbFcpU\nhQZu/7DJuIFtf9Tx6pUxH1NEtUd1v/TZLMJW8Nzc4wlZPAPjODptTm5zLjIu0MY+\nU0XEnyNZncZRqSQ7kUtd3/JfpamPtkRYtZ2w8z1WEilLmahcONx8BgeZtiXc/IDQ\nXs2+rZJ0KFANHeUj9xK+dgQ5XWyaMz7NySIYgfZTeVs=\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIC7DCCAdSgAwIBAgIUWPMMxFcabglBM3m+tZt5z/8xNC0wDQYJKoZIhvcNAQEL\nBQAwFjEUMBIGA1UEAwwLY3JkdmFsLWNhLTUwHhcNMjYxMTI1MDkxNDMxWhcNMjYx\nMTI3MDkxNDMxWjAWMRQwEgYDVQQDDAtjcmR2YWwtY2EtNTCCASIwDQYJKoZIhvcN\nAQEBBQADggEPADCCAQoCggEBAL+V23oN6p/PPEJE86y5N9+IPONxQp74egPmRQIO\nMtzFOcbYd25waLPWe0Rf7HDR6Zqu+BnTmJqUretHhAMy1+XQjHzHNR3t3bbs6KSP\n+Y1p/69HjSaTxWEJGaM/Nm5lg5fUqOQxnNDhumoBd15+ZdlyaJOxPjjkTMhBpchC\nKyvs98lScWd4UuI49YtJNrWg1YHtm8ucMHkcK1pCG20wiJTpx6fMyUqS+IL/9n2L\nf/bbvYUc0QO29bxbYdRK/hYJjOK7ZIoQrYq7L/vuW6DAwd1IqhEHIdgRmEhihdi8\n1TdYgNztZSufZKgzWr+gYQ1mLEEZGB02PciKh07q3sDyhu8CAwEAAaMyMDAwHQYD\nVR0OBBYEFOiE9o3XUdXLXa2CkmRfzjzeH9mPMA8GA1UdEwEB/wQFMAMBAf8wDQYJ\nKoZIhvcNAQELBQADggEBAA6OF5bqRLwZTBYerUkathXMWsZKAO7JaFxtgtUeJEkJ\n36fXcZRctHwGvrWyaddzRdAQq3HWU8TYOP6bdqjvfVkHk1J6T4lv1OxIS7lBZZsf\nNkK4yC1UtVu923TlNyGWkTaDo8bbF1n9KrZBsnx7fUz8j9yZ5a0m4rSCWQkn+p9n\nGL9D8vJKPGbwvq/OnV6xfgyQBzn1IbpZ/TNc29oy0r7YBEz/9no9gHwCfFrBJb1L\neN/x2fiWrJa7GQ6VZW8YuxFwwMmSecrIFj1zYt4AZzyqTFNx6UWB8KPoBF5w6BWn\nsvgZfl5vB+sLPfUZjxA4MwB9OXh9FPtGO8hRz6z/beE=\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIC7DCCAdSgAwIBAgIUExcBBVIpJUVqp11S9W6m9NaP0DgwDQYJKoZIhvcNAQEL\nBQAwFjEUMBIGA1UEAwwLY3JkdmFsLWNhLTYwHhcNMjYxMTI1MDkxNDMxWhcNMjYx\nMTI3MDkxNDMxWjAWMRQwEgYDVQQDDAtjcmR2YWwtY2EtNjCCASIwDQYJKoZIhvcN\nAQEBBQADggEPADCCAQoCggEBAJWEW5iFVMDXKeAc+jV8+4B8T0O6PplAtznu24Hx\n98UBK+RU0I5J4fGpzGfmgZK/pa/QwegDxE6B/BDdXlHYoCJpYbGaePU+CZmrFAC5\n0ssmfFrey8Q04yeWPJhGngeDLs9/eyIIc5eAetU7wAWCzI7NdPfRGAQqbnMFUgaB\nEtKi7tPzxID2IOksOKSvla2qXCfri+rnd+FrmsS7Hyw8CIxMtnGUhuhvLuehnqz2\n/CQRM5tQO87jsvAv4vr6OTpWof7iG7cKmqbkPHGUWRFDF9WQYr06HrTliPrhrmb4\nt1gjTAB8Cjr5pHVx4yWgyn3r2l/Fv52IihdlKfvGAh/dLrkCAwEAAaMyMDAwHQYD\nVR0OBBYEFMwgAZ95mBeU3Qayi3f97/N3YkjsMA8GA1UdEwEB/wQFMAMBAf8wDQYJ\nKoZIhvcNAQELBQADggEBAIHCDHSjcTMuvA94Xvo4t/9qAyUxxY6Lb+pdJG63WVrR\nijmrGZUqXq1D+OrJAkteLN84s1PwT+J16uqzRc2cnzUvsRm7FQ3Wkj1X9v4YtYQy\ngRNFBSOiMJRWZ3jGpPfI8pdwlDlHkWWZiqSBZoxD+Rh1cvE17GqANiLfG7jZ8yLm\nDnM/aTo5iVzjtAHHrnWTvhzQ5GyZQxj2ej/mj4vfl+Hqcm/BIfsSbWMachhVdLf1\njO1UohUXLT10XroAAxcxLPmNaXr9ML3xAG9Zou9yXDq6m6ON9cK7chW5ZHiPpzGC\nQhZQVLCyq5gthC8sRkbobUP09QkvObAE5tYnRKJ9MVk=\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIC7DCCAdSgAwIBAgIUUQ9rfpnEwg9a8CL/qg9roiCSbNswDQYJKoZIhvcNAQEL\nBQAwFjEUMBIGA1UEAwwLY3JkdmFsLWNhLTcwHhcNMjYxMTI1MDkxNDMxWhcNMjYx\nMTI3MDkxNDMxWjAWMRQwEgYDVQQDDAtjcmR2YWwtY2EtNzCCASIwDQYJKoZIhvcN\nAQEBBQADggEPADCCAQoCggEBAOTPDc34cohoDjG9+Bu3VLHsN50LX1Mo3Q2ITVO+\ndrLHY4CBKec1aIaRZNhp6I+ZI72dQd2rSs6cnOgUHVmMo+V5r5cJ/c488pERFPRQ\nWulZoPTzf+3DhJGatdp2JSfxzHH/jIRNFYiGIcERFrlB9JMDjIR4ziC3HKFhRf+X\nSZJEb29pIhrJHqLGtKhXrr0IxrV7aMUJLBF5LZ5SIGPYOdQJUfz3sk5Ix62+RNc2\nKrmI5YSc7CEC0VRqO/TmUeds+NwhKmIW5eDaxuJmKjBifQYDCft4cDB51rumeQy1\nFkICh+Wt2VNntC697ktig/Z5sDbBBCGnufdHUCbGYcypE/sCAwEAAaMyMDAwHQYD\nVR0OBBYEFIvKsHJoxiqmb4Adqt7tro99aybKMA8GA1UdEwEB/wQFMAMBAf8wDQYJ\nKoZIhvcNAQELBQADggEBABbW9MBmICotd6YnYwjKZMTlOTjqmjUS0kp6uCvuMps0\nSN7xnPkqiyUSYb52ZHrrR03pr5s9OVj7PWfknSgBWHnUQhReTynR+lV9GrX1WzTg\n+NJqEwm2FhUof0XOSMk3KbXNV+P3NPztrBiBc4May1lCSfdoX9uQH1UP+CfaBf9E\nBHifbNg/QCu8mH2usTHLaBDlxAYeINIwSbqtWdVzIM8V+jS8cv1YivlBlRLzk94U\nzWS5JwI4M29TIzLZ55WxHuS2I6p9G7zMSsVtR2hrtNIb3kNfJqz2mX9PNIoLDiDO\nqzNpDNJoLi5KjDV8E9l3TCNUGm/BaJrCbvZSNOd+1Ms=\n-----END CERTIFICATE-----\n"
	// avroSchemaOver253 is an Avro schema of 545 characters.
	avroSchemaOver253 = "{\"type\": \"record\", \"name\": \"Order\", \"namespace\": \"com.example\", \"fields\": [{\"name\": \"field_number_00\", \"type\": \"string\"}, {\"name\": \"field_number_01\", \"type\": \"string\"}, {\"name\": \"field_number_02\", \"type\": \"string\"}, {\"name\": \"field_number_03\", \"type\": \"string\"}, {\"name\": \"field_number_04\", \"type\": \"string\"}, {\"name\": \"field_number_05\", \"type\": \"string\"}, {\"name\": \"field_number_06\", \"type\": \"string\"}, {\"name\": \"field_number_07\", \"type\": \"string\"}, {\"name\": \"field_number_08\", \"type\": \"string\"}, {\"name\": \"field_number_09\", \"type\": \"string\"}]}"
	// principalMappingOver253 is an expression of 301 characters.
	principalMappingOver253 = "{{ context.certificate.subject.cn + 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx' }}"
)

// TestEventGatewayStringLengths sends raw JSON, as kubectl does.
func TestEventGatewayStringLengths(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)

	tests := []struct {
		name    string
		base    string
		path    []string
		value   any
		wantErr string
	}{
		{name: "inline Avro schema over 253 characters", base: "encryptPolicy", path: []string{"apiSpec"}, value: map[string]any{
			"type": "schemaValidation",
			"schemaValidation": map[string]any{"name": "validate", "config": map[string]any{
				"type": "inlineSchema", "inlineSchema": map[string]any{"valueValidationAction": "reject", "value": map[string]any{"schemaType": "avro", "avro": map[string]any{"schema": avroSchemaOver253}}},
			}},
		}},
		{name: "trust bundle of 7 certificates", base: "trustBundle", path: []string{"apiSpec", "config", "trustedCa"}, value: map[string]any{"type": "inline", "value": caBundleOf7}},
		{name: "backend cluster CA bundle of 7 certificates", base: "backendCluster", path: []string{"apiSpec", "tls", "caBundle"}, value: caBundleOf7},
		{name: "principalMapping over 253 characters", base: "tlsListenerPolicy", path: []string{"apiSpec", "tlsServer", "config", "clientAuthentication", "principalMapping"}, value: principalMappingOver253},
		{name: "name over the OAS maximum of 255", base: "backendCluster", path: []string{"apiSpec", "name"}, value: strings.Repeat("a", 256),
			wantErr: "spec.apiSpec.name: Too long: may not be more than 255 bytes"},
		{name: "enum still refuses other values", base: "decryptPolicy", path: []string{"apiSpec", "decrypt", "config", "failureMode"}, value: "other",
			wantErr: "Unsupported value: \"other\""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest, ok := eventGatewayNameBases[tc.base]
			if !ok {
				manifest = eventGatewayArrayBases[tc.base]
			}
			require.NotEmpty(t, manifest, tc.base)
			obj := &unstructured.Unstructured{}
			require.NoError(t, yaml.Unmarshal([]byte(manifest), &obj.Object))
			obj.SetAPIVersion("configuration.konghq.com/v1alpha1")
			obj.SetNamespace(ns.Name)
			obj.SetGenerateName("length-")
			require.NoError(t, unstructured.SetNestedField(obj.Object, tc.value, append([]string{"spec"}, tc.path...)...))
			err := cl.Create(ctx, obj, client.DryRunAll)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
