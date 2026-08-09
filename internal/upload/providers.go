package upload

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"NyaMediaMetadataTool/internal/store"
)

// ProviderDescriptor is the stable capability catalog exposed to the UI and
// future integrations. A descriptor may be visible before its implementation
// is installed, so configuration can be prepared without silently pretending
// that uploads are supported.
type ProviderDescriptor struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Implemented bool                   `json:"implemented"`
	SecretKeys  []string               `json:"secretKeys,omitempty"`
	AuthDevices []AuthDeviceDescriptor `json:"authDevices,omitempty"`
}

type AuthDeviceDescriptor struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

var defaultProviderDescriptors = map[string]ProviderDescriptor{
	store.UploadProviderType115Cookie: {
		Type:        store.UploadProviderType115Cookie,
		Name:        "115 Cookie",
		Implemented: true,
		SecretKeys:  []string{"cookie"},
		AuthDevices: []AuthDeviceDescriptor{
			{Code: "web", Name: "网页端"},
			{Code: "android", Name: "Android"},
			{Code: "ios", Name: "iOS"},
			{Code: "tv", Name: "电视端"},
			{Code: "alipaymini", Name: "支付宝小程序"},
			{Code: "wechatmini", Name: "微信小程序"},
			{Code: "qandroid", Name: "115组织 Android"},
		},
	},
	store.UploadProviderType115Open: {
		Type:        store.UploadProviderType115Open,
		Name:        "115 Open",
		Implemented: false,
		SecretKeys:  []string{"client_id", "access_token", "refresh_token", "access_token_expires_at"},
	},
	store.UploadProviderType123Pan: {
		Type:        store.UploadProviderType123Pan,
		Name:        "123 云盘",
		Implemented: false,
		SecretKeys:  []string{"access_token", "refresh_token"},
	},
	store.UploadProviderTypeBaiduPan: {
		Type:        store.UploadProviderTypeBaiduPan,
		Name:        "百度网盘 Open",
		Implemented: true,
		SecretKeys:  []string{"client_id", "client_secret", "access_token", "refresh_token", "access_token_expires_at"},
	},
}

func ListProviderDescriptors() []ProviderDescriptor {
	return sortedProviderDescriptors(providerDescriptorMap())
}

func providerDescriptorMap() map[string]ProviderDescriptor {
	result := make(map[string]ProviderDescriptor, len(defaultProviderDescriptors))
	for providerType, descriptor := range defaultProviderDescriptors {
		descriptor.SecretKeys = append([]string{}, descriptor.SecretKeys...)
		descriptor.AuthDevices = append([]AuthDeviceDescriptor{}, descriptor.AuthDevices...)
		result[providerType] = descriptor
	}
	return result
}

func sortedProviderDescriptors(descriptors map[string]ProviderDescriptor) []ProviderDescriptor {
	result := make([]ProviderDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		descriptor.SecretKeys = append([]string{}, descriptor.SecretKeys...)
		descriptor.AuthDevices = append([]AuthDeviceDescriptor{}, descriptor.AuthDevices...)
		result = append(result, descriptor)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Type < result[j].Type })
	return result
}

type SecretLookup func(ctx context.Context, key string) (string, error)

// ProviderBuilder is the extension point for future remote drives. Builders
// receive a target snapshot and a scoped secret lookup, keeping credentials
// out of the queue and provider-neutral interfaces.
type ProviderBuilder func(ctx context.Context, target store.UploadBatchTarget, lookup SecretLookup) (Provider, error)

func normalizeProviderType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func unsupportedProviderError(providerType string) error {
	descriptor, ok := defaultProviderDescriptors[normalizeProviderType(providerType)]
	if ok && !descriptor.Implemented {
		return fmt.Errorf("upload provider type %q is not installed yet", providerType)
	}
	return fmt.Errorf("upload provider type %q is not installed", providerType)
}
