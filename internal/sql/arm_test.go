package sql

import (
	"context"
	"net/http"
	"strings"
	"testing"

	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql/v2/fake"
	"github.com/google/go-cmp/cmp"
)

var allowUnexported = cmp.AllowUnexported(dbCaps{}, serviceObjective{}, availability{})

func TestNewARMSource(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		a, err := NewARMSource(testSubscriptionID, &azfake.TokenCredential{}, policy.ClientOptions{}, nil)
		if err != nil || a == nil {
			t.Errorf("NewARMSource() = %v, %v, want a source and no error", a, err)
		}
	})

	t.Run("invalid subscription", func(t *testing.T) {
		t.Parallel()

		_, err := NewARMSource("not-a-guid", &azfake.TokenCredential{}, policy.ClientOptions{}, nil)
		if err == nil || !strings.Contains(err.Error(), "not a valid subscription ID") {
			t.Errorf("NewARMSource(%q) error = %v, want an invalid subscription error", "not-a-guid", err)
		}
	})
}

func sdkSize(limit int32, unit armsql.MaxSizeUnit) *armsql.MaxSizeCapability {
	return &armsql.MaxSizeCapability{Limit: new(limit), Unit: new(unit)}
}

func sizeRange(lo, hi *armsql.MaxSizeCapability, status armsql.CapabilityStatus) *armsql.MaxSizeRangeCapability {
	return &armsql.MaxSizeRangeCapability{MinValue: lo, MaxValue: hi, Status: new(status)}
}

// sdkObjective is an SDK service objective in the shape Azure returns.
func sdkObjective(name, skuName, family string, vcores float64, status armsql.CapabilityStatus) *armsql.ServiceObjectiveCapability {
	return &armsql.ServiceObjectiveCapability{
		Name:             new(name),
		Status:           new(status),
		ComputeModel:     new("Provisioned"),
		SKU:              &armsql.SKU{Name: new(skuName), Family: new(family)},
		PerformanceLevel: &armsql.PerformanceLevelCapability{Unit: new(armsql.PerformanceLevelUnitVCores), Value: new(vcores)},
		SupportedMaxSizes: []*armsql.MaxSizeRangeCapability{
			sizeRange(sdkSize(1, armsql.MaxSizeUnitGigabytes), sdkSize(1, armsql.MaxSizeUnitTerabytes), armsql.CapabilityStatusAvailable),
			sizeRange(sdkSize(1, armsql.MaxSizeUnitGigabytes), sdkSize(4, armsql.MaxSizeUnitTerabytes), armsql.CapabilityStatusDisabled),
		},
		ZoneRedundant: new(true),
	}
}

func TestConvertDBCaps(t *testing.T) {
	t.Parallel()

	storage := []*armsql.StorageCapability{
		{StorageAccountType: new(armsql.StorageCapabilityStorageAccountTypeGRS), Status: new(armsql.CapabilityStatusDefault)},
		{StorageAccountType: new(armsql.StorageCapabilityStorageAccountTypeGZRS), Status: new(armsql.CapabilityStatusVisible)},
		nil,
	}

	lc := armsql.LocationCapabilities{
		Name:   new("UK South"),
		Status: new(armsql.CapabilityStatusAvailable),
		SupportedServerVersions: []*armsql.ServerVersionCapability{
			{
				Name:   new("12.0"),
				Status: new(armsql.CapabilityStatusDefault),
				SupportedEditions: []*armsql.EditionCapability{
					{
						Name:                         new("GeneralPurpose"),
						Status:                       new(armsql.CapabilityStatusDefault),
						SupportedStorageCapabilities: storage,
						SupportedServiceLevelObjectives: []*armsql.ServiceObjectiveCapability{
							sdkObjective("GP_Gen5_2", "GP_Gen5", "Gen5", 2, armsql.CapabilityStatusAvailable),
							sdkObjective("GP_Fsv2_8", "GP_Fsv2", "Fsv2", 8, armsql.CapabilityStatusVisible),
							nil,
						},
					},
					{
						Name:   new("Hyperscale"),
						Status: new(armsql.CapabilityStatusDisabled),
						Reason: new("not here"),
						SupportedServiceLevelObjectives: []*armsql.ServiceObjectiveCapability{
							{Name: new("HS_Gen5_2"), Status: new(armsql.CapabilityStatusAvailable)},
						},
					},
					nil,
				},
			},
			// A second version lists GP_Gen5_2 again: the first, deployable,
			// one is kept.
			{
				Name:   new("13.0"),
				Status: new(armsql.CapabilityStatusVisible),
				SupportedEditions: []*armsql.EditionCapability{{
					Name: new("GeneralPurpose"),
					SupportedServiceLevelObjectives: []*armsql.ServiceObjectiveCapability{
						sdkObjective("GP_Gen5_2", "GP_Gen5", "Gen5", 2, armsql.CapabilityStatusAvailable),
					},
				}},
			},
			nil,
		},
	}

	got := convertDBCaps(lc)

	want := dbCaps{
		location: deployable,
		objectives: []serviceObjective{
			{
				name: "GP_Gen5_2", skuName: "GP_Gen5", edition: "GeneralPurpose", family: "Gen5",
				computeModel: "Provisioned", unit: "VCores", performance: 2,
				maxStorageGiB: new(1024.0), zoneRedundant: true, backup: []string{"GRS"}, availability: deployable,
			},
			{
				name: "GP_Fsv2_8", skuName: "GP_Fsv2", edition: "GeneralPurpose", family: "Fsv2",
				computeModel: "Provisioned", unit: "VCores", performance: 8,
				maxStorageGiB: new(1024.0), zoneRedundant: true, backup: []string{"GRS"},
				availability: availability{reason: "SKU GP_Fsv2_8 is Visible"},
			},
			{
				name: "HS_Gen5_2", edition: "Hyperscale", backup: []string{},
				availability: availability{reason: "edition Hyperscale is Disabled: not here"},
			},
		},
	}

	if diff := cmp.Diff(want, got, allowUnexported); diff != "" {
		t.Errorf("convertDBCaps() mismatch (-want +got):\n%s", diff)
	}
}

func TestConvertDBCaps_locationRestricted(t *testing.T) {
	t.Parallel()

	lc := armsql.LocationCapabilities{
		Name:   new("West Europe"),
		Status: new(armsql.CapabilityStatusVisible),
		Reason: new("Provisioning is restricted in this region."),
		SupportedServerVersions: []*armsql.ServerVersionCapability{{
			Name: new("12.0"),
			SupportedEditions: []*armsql.EditionCapability{{
				Name: new("GeneralPurpose"),
				SupportedServiceLevelObjectives: []*armsql.ServiceObjectiveCapability{
					sdkObjective("GP_Gen5_2", "GP_Gen5", "Gen5", 2, armsql.CapabilityStatusDefault),
				},
			}},
		}},
	}

	got := convertDBCaps(lc)

	want := availability{reason: "location West Europe is Visible: Provisioning is restricted in this region."}

	if diff := cmp.Diff(want, got.location, allowUnexported); diff != "" {
		t.Errorf("convertDBCaps() location mismatch (-want +got):\n%s", diff)
	}

	if len(got.objectives) != 1 || got.objectives[0].availability != want {
		t.Errorf("convertDBCaps() objectives = %+v, want one restricted by the location", got.objectives)
	}
}

func TestGibOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size *armsql.MaxSizeCapability
		want *float64
	}{
		{name: "megabytes", size: sdkSize(262144, armsql.MaxSizeUnitMegabytes), want: new(256.0)},
		{name: "gigabytes", size: sdkSize(250, armsql.MaxSizeUnitGigabytes), want: new(250.0)},
		{name: "terabytes", size: sdkSize(4, armsql.MaxSizeUnitTerabytes), want: new(4096.0)},
		{name: "petabytes", size: sdkSize(1, armsql.MaxSizeUnitPetabytes), want: new(1048576.0)},
		{name: "unknown unit", size: sdkSize(1, "Exabytes")},
		{name: "no limit", size: &armsql.MaxSizeCapability{Unit: new(armsql.MaxSizeUnitGigabytes)}},
		{name: "nil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, gibOf(tc.size)); diff != "" {
				t.Errorf("gibOf() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestARMSource_listDBCapabilities(t *testing.T) {
	t.Parallel()

	var include armsql.CapabilityGroup

	srv := &fake.CapabilitiesServer{
		ListByLocation: func(_ context.Context, location string, opts *armsql.CapabilitiesClientListByLocationOptions) (
			resp azfake.Responder[armsql.CapabilitiesClientListByLocationResponse], errResp azfake.ErrorResponder,
		) {
			if opts != nil && opts.Include != nil {
				include = *opts.Include
			}

			resp.SetResponse(http.StatusOK, armsql.CapabilitiesClientListByLocationResponse{LocationCapabilities: armsql.LocationCapabilities{
				Name: new(location),
				SupportedServerVersions: []*armsql.ServerVersionCapability{{
					SupportedEditions: []*armsql.EditionCapability{{
						Name: new("GeneralPurpose"),
						SupportedServiceLevelObjectives: []*armsql.ServiceObjectiveCapability{
							sdkObjective("GP_Gen5_2", "GP_Gen5", "Gen5", 2, armsql.CapabilityStatusAvailable),
						},
					}},
				}},
			}}, nil)

			return resp, errResp
		},
	}

	got, err := newTestARMSource(t, srv, nil).listDBCapabilities(t.Context(), "uksouth")
	if err != nil {
		t.Fatalf("listDBCapabilities() error = %v", err)
	}

	if include != armsql.CapabilityGroupSupportedEditions {
		t.Errorf("listDBCapabilities() asked to include %q, want %q", include, armsql.CapabilityGroupSupportedEditions)
	}

	if len(got.objectives) != 1 || got.objectives[0].name != "GP_Gen5_2" {
		t.Errorf("listDBCapabilities() objectives = %+v, want GP_Gen5_2", got.objectives)
	}
}

func TestARMSource_listCapabilities_error(t *testing.T) {
	t.Parallel()

	srv := &fake.CapabilitiesServer{
		ListByLocation: func(context.Context, string, *armsql.CapabilitiesClientListByLocationOptions) (
			resp azfake.Responder[armsql.CapabilitiesClientListByLocationResponse], errResp azfake.ErrorResponder,
		) {
			errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")

			return resp, errResp
		},
	}

	if _, err := newTestARMSource(t, srv, nil).listDBCapabilities(t.Context(), "uksouth"); err == nil || !strings.Contains(err.Error(), "listing SQL Database capabilities") {
		t.Errorf("listDBCapabilities() error = %v, want a wrapped error", err)
	}
}

func usage(name string, used, limit float64) *armsql.SubscriptionUsage {
	return &armsql.SubscriptionUsage{
		Name:       new(name),
		Properties: &armsql.SubscriptionUsageProperties{CurrentValue: new(used), Limit: new(limit)},
	}
}

func TestARMSource_listQuota(t *testing.T) {
	t.Parallel()

	srv := &fake.SubscriptionUsagesServer{
		NewListByLocationPager: func(string, *armsql.SubscriptionUsagesClientListByLocationOptions) (
			resp azfake.PagerResponder[armsql.SubscriptionUsagesClientListByLocationResponse],
		) {
			resp.AddPage(http.StatusOK, armsql.SubscriptionUsagesClientListByLocationResponse{SubscriptionUsageListResult: armsql.SubscriptionUsageListResult{
				Value: []*armsql.SubscriptionUsage{
					usage(regionalVCores, 10, 150),
					{Name: new("NoProperties")},
					nil,
				},
			}}, nil)
			resp.AddPage(http.StatusOK, armsql.SubscriptionUsagesClientListByLocationResponse{SubscriptionUsageListResult: armsql.SubscriptionUsageListResult{
				Value: []*armsql.SubscriptionUsage{usage(serverQuota, 1, 250)},
			}}, nil)

			return resp
		},
	}

	got, err := newTestARMSource(t, nil, srv).listQuota(t.Context(), "uksouth")
	if err != nil {
		t.Fatalf("listQuota() error = %v", err)
	}

	want := map[string]quota{
		regionalVCores: {Used: 10, Limit: 150},
		serverQuota:    {Used: 1, Limit: 250},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("listQuota() mismatch (-want +got):\n%s", diff)
	}
}

func TestARMSource_listQuota_error(t *testing.T) {
	t.Parallel()

	srv := &fake.SubscriptionUsagesServer{
		NewListByLocationPager: func(string, *armsql.SubscriptionUsagesClientListByLocationOptions) (
			resp azfake.PagerResponder[armsql.SubscriptionUsagesClientListByLocationResponse],
		) {
			resp.AddResponseError(http.StatusForbidden, "AuthorizationFailed")

			return resp
		},
	}

	_, err := newTestARMSource(t, nil, srv).listQuota(t.Context(), "uksouth")

	if err == nil || !strings.Contains(err.Error(), "listing SQL usage") {
		t.Errorf("listQuota() error = %v, want a wrapped error", err)
	}
}
