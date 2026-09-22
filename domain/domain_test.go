package foreignpassengersupport

import (
	"encoding/json"
	"os"
	"testing"
)

func loadFixture(t *testing.T) Record {
	t.Helper()
	raw, err := os.ReadFile("../fixtures/domain.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFixtureMatchesDomain(t *testing.T) {
	value := loadFixture(t)
	if value.Domain != "foreign-passenger-support" {
		t.Fatalf("领域标识不一致: %s", value.Domain)
	}
	if value.Version != 2 {
		t.Fatalf("示例资料应为 v2: %d", value.Version)
	}
}

func TestFixtureChannelsCoverHandoverPath(t *testing.T) {
	value := loadFixture(t)
	kinds := map[string]bool{}
	for _, channel := range value.Channels {
		kinds[channel.Kind] = true
	}
	for _, required := range []string{"online_staff", "station_window", "offline_backfill"} {
		if !kinds[required] {
			t.Fatalf("接续路径缺少渠道: %s", required)
		}
	}
}

func TestFixtureConsentBoundaries(t *testing.T) {
	value := loadFixture(t)
	if !value.Consent.Revocable {
		t.Fatal("授权必须可撤回")
	}
	decisions := map[string]bool{}
	for _, scope := range value.Consent.Scopes {
		decisions[scope.Category] = scope.Allowed
	}
	for _, category := range []string{
		CategoryDocumentVersion, CategoryVerification, CategoryTicketIdentity,
		CategoryTripLink, CategoryMultilingual, CategoryManualHandling,
	} {
		if !decisions[category] {
			t.Fatalf("接续类别 %s 应已获本人授权", category)
		}
	}
	if decisions[CategoryUnrelatedVisa] {
		t.Fatal("与本次出行无关的签证资料不得被授权复制")
	}
}

func TestFixtureDisambiguationRequiresHuman(t *testing.T) {
	value := loadFixture(t)
	if !value.Disambiguation.LowConfidenceToHuman {
		t.Fatal("低置信度情况必须交由人工确认")
	}
	if value.Disambiguation.ConfidenceFloor < 0.9 || value.Disambiguation.ConfidenceFloor > 1 {
		t.Fatalf("置信度门槛不合理: %v", value.Disambiguation.ConfidenceFloor)
	}
	covered := map[string]bool{}
	for _, rule := range value.Disambiguation.Rules {
		if !rule.RequiresHuman {
			t.Fatalf("场景 %s 必须保留人工确认", rule.Case)
		}
		covered[rule.Case] = true
	}
	for _, required := range []string{
		CaseNameSimilar, CaseTravelingFamily, CaseCrossBorderTrip, CaseOfflineWindowBackfill,
	} {
		if !covered[required] {
			t.Fatalf("缺少审慎场景: %s", required)
		}
	}
}

func TestFixturePrivacyAndAudit(t *testing.T) {
	value := loadFixture(t)
	if !contains(value.Privacy.ExcludedData, CategoryUnrelatedVisa) {
		t.Fatal("隐私控制必须排除与本次出行无关的签证资料")
	}
	for _, visible := range value.Privacy.StationView {
		if contains(value.Privacy.ExcludedData, visible) {
			t.Fatalf("车站最小可见范围包含已排除资料: %s", visible)
		}
	}
	for _, risk := range []string{RiskWrongMerge, RiskUnauthorizedView, RiskOverRetention} {
		if !contains(value.Audit.DetectableRisks, risk) {
			t.Fatalf("审计缺少风险发现: %s", risk)
		}
	}
	if !contains(value.Audit.TraceableFrom, SourceRefundChange) ||
		!contains(value.Audit.TraceTargets, TargetActualConsent) ||
		!contains(value.Audit.TraceTargets, TargetVerificationLog) {
		t.Fatal("审计必须支持从退改结果回溯真实授权与核验过程")
	}
}

// mutatedFixture 在内存中修改示例资料后重新解析，用于验证规则会拒绝违规资料。
func mutatedFixture(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile("../fixtures/domain.json")
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	mutate(data)
	out, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParseRejectsRuleViolations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "接续无需同意",
			mutate: func(data map[string]any) {
				data["continuity"].(map[string]any)["require_consent"] = false
			},
		},
		{
			name: "缺少核验结果接续",
			mutate: func(data map[string]any) {
				carry := data["continuity"].(map[string]any)["carry_on"].([]any)
				filtered := carry[:0]
				for _, item := range carry {
					if item != CategoryVerification {
						filtered = append(filtered, item)
					}
				}
				data["continuity"].(map[string]any)["carry_on"] = filtered
			},
		},
		{
			name: "签证资料被授权复制",
			mutate: func(data map[string]any) {
				scopes := data["consent"].(map[string]any)["scopes"].([]any)
				for _, item := range scopes {
					scope := item.(map[string]any)
					if scope["category"] == CategoryUnrelatedVisa {
						scope["allowed"] = true
					}
				}
			},
		},
		{
			name: "撤回后无最小可见说明",
			mutate: func(data map[string]any) {
				data["privacy"].(map[string]any)["after_consent_revoked"] = ""
			},
		},
		{
			name: "缺少姓名近似审慎场景",
			mutate: func(data map[string]any) {
				rules := data["disambiguation"].(map[string]any)["rules"].([]any)
				filtered := rules[:0]
				for _, item := range rules {
					if item.(map[string]any)["case"] != CaseNameSimilar {
						filtered = append(filtered, item)
					}
				}
				data["disambiguation"].(map[string]any)["rules"] = filtered
			},
		},
		{
			name: "低置信度不转人工",
			mutate: func(data map[string]any) {
				data["disambiguation"].(map[string]any)["low_confidence_to_human"] = false
			},
		},
		{
			name: "置信度门槛越界",
			mutate: func(data map[string]any) {
				data["disambiguation"].(map[string]any)["confidence_floor"] = 1.2
			},
		},
		{
			name: "车站可见已排除资料",
			mutate: func(data map[string]any) {
				view := data["privacy"].(map[string]any)["station_view_minimum"].([]any)
				data["privacy"].(map[string]any)["station_view_minimum"] = append(view, CategoryUnrelatedVisa)
			},
		},
		{
			name: "审计无法发现错误合并",
			mutate: func(data map[string]any) {
				risks := data["audit"].(map[string]any)["detectable_risks"].([]any)
				filtered := risks[:0]
				for _, item := range risks {
					if item != RiskWrongMerge {
						filtered = append(filtered, item)
					}
				}
				data["audit"].(map[string]any)["detectable_risks"] = filtered
			},
		},
		{
			name: "退改结果无法回溯授权",
			mutate: func(data map[string]any) {
				targets := data["audit"].(map[string]any)["trace_targets"].([]any)
				filtered := targets[:0]
				for _, item := range targets {
					if item != TargetActualConsent {
						filtered = append(filtered, item)
					}
				}
				data["audit"].(map[string]any)["trace_targets"] = filtered
			},
		},
		{
			name: "缺少离线窗口渠道",
			mutate: func(data map[string]any) {
				channels := data["channels"].([]any)
				filtered := channels[:0]
				for _, item := range channels {
					if item.(map[string]any)["kind"] != "offline_backfill" {
						filtered = append(filtered, item)
					}
				}
				data["channels"] = filtered
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(mutatedFixture(t, tc.mutate)); err == nil {
				t.Fatalf("违规资料应被拒绝: %s", tc.name)
			}
		})
	}
}

func TestParseV1RecordWithoutStructuredSections(t *testing.T) {
	raw := []byte(`{
        "domain": "foreign-passenger-support",
        "version": 1,
        "sample_id": "record-legacy",
        "actors": ["外籍铁路旅客", "铁路客服中心"],
        "facts": ["事实一", "事实二"],
        "constraints": ["约束一", "约束二"]
    }`)
	if _, err := Parse(raw); err != nil {
		t.Fatalf("v1 基础资料应保持可读: %v", err)
	}
}
