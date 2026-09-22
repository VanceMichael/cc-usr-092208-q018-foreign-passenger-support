package foreignpassengersupport

import (
	"encoding/json"
	"errors"
	"strings"
)

// 接续时允许跨渠道携带的资料类别（须经本人同意）。
const (
	CategoryDocumentVersion = "证件版本"
	CategoryVerification    = "核验结果"
	CategoryTicketIdentity  = "购票身份"
	CategoryTripLink        = "行程关联"
	CategoryMultilingual    = "多语种求助"
	CategoryManualHandling  = "人工处置"
)

// 即使本人同意也不得复制的资料类别。
const CategoryUnrelatedVisa = "与本次出行无关的签证资料"

// 消歧审慎规则覆盖的场景。
const (
	CaseNameSimilar           = "姓名近似"
	CaseTravelingFamily       = "同行家庭"
	CaseCrossBorderTrip       = "跨境行程"
	CaseOfflineWindowBackfill = "离线窗口补录"
)

// 审计必须能够发现的风险与回溯链路。
const (
	RiskWrongMerge        = "错误合并"
	RiskUnauthorizedView  = "越权查阅"
	RiskOverRetention     = "超期保留"
	SourceRefundChange    = "退改结果"
	TargetActualConsent   = "真实授权"
	TargetVerificationLog = "核验过程"
)

// Record 表示项目共享的领域资料。
type Record struct {
	Domain      string   `json:"domain"`
	Version     int      `json:"version"`
	SampleID    string   `json:"sample_id"`
	Actors      []string `json:"actors"`
	Facts       []string `json:"facts"`
	Constraints []string `json:"constraints"`

	Channels       []Channel      `json:"channels"`
	Continuity     Continuity     `json:"continuity"`
	Consent        Consent        `json:"consent"`
	Disambiguation Disambiguation `json:"disambiguation"`
	Privacy        Privacy        `json:"privacy"`
	Audit          Audit          `json:"audit"`
}

// Channel 描述旅客可以发起或延续请求的服务渠道。
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // online_staff | station_window | offline_backfill
}

// Continuity 规定跨渠道接续时携带什么、以什么为前提。
type Continuity struct {
	Handover                  string   `json:"handover"`
	CarryOn                   []string `json:"carry_on"`
	RequireConsent            bool     `json:"require_consent"`
	SameRequestAcrossChannels bool     `json:"same_request_across_channels"`
}

// Consent 规定本人同意的范围、可撤回性与撤回后果。
type Consent struct {
	Holder       string  `json:"holder"`
	Scopes       []Scope `json:"scopes"`
	Revocable    bool    `json:"revocable"`
	OnRevocation string  `json:"on_revocation"`
}

// Scope 是单个资料类别的授权项。
type Scope struct {
	Category string `json:"category"`
	Purpose  string `json:"purpose"`
	Allowed  bool   `json:"allowed"`
}

// Disambiguation 规定高风险匹配场景的审慎规则与人工确认门槛。
type Disambiguation struct {
	Rules                []DisambiguationRule `json:"rules"`
	ConfidenceFloor      float64              `json:"confidence_floor"`
	LowConfidenceToHuman bool                 `json:"low_confidence_to_human"`
}

// DisambiguationRule 是单个场景下的审慎处置规则。
type DisambiguationRule struct {
	Case          string `json:"case"`
	Policy        string `json:"policy"`
	RequiresHuman bool   `json:"requires_human"`
}

// Privacy 规定最小可见范围、排除资料、保留期限与证件更新后的处理。
type Privacy struct {
	StationView         []string `json:"station_view_minimum"`
	ExcludedData        []string `json:"excluded_data"`
	RetentionLimit      string   `json:"retention_limit"`
	AfterDocumentUpdate string   `json:"after_document_update"`
	AfterConsentRevoked string   `json:"after_consent_revoked"`
}

// Audit 规定必须可发现的风险与从退改结果回溯授权、核验的链路。
type Audit struct {
	DetectableRisks []string `json:"detectable_risks"`
	TraceableFrom   []string `json:"traceable_from"`
	TraceTargets    []string `json:"trace_targets"`
}

// Parse 读取并检查带版本的业务资料。
func Parse(raw []byte) (Record, error) {
	var value Record
	if err := json.Unmarshal(raw, &value); err != nil {
		return Record{}, err
	}
	if value.Domain == "" || value.Version < 1 || value.SampleID == "" ||
		len(value.Actors) < 2 || len(value.Facts) < 2 || len(value.Constraints) < 2 {
		return Record{}, errors.New("共享资料缺少必要字段")
	}
	if value.Version >= 2 {
		if err := value.validateV2(); err != nil {
			return Record{}, err
		}
	}
	return value, nil
}

func (r Record) validateV2() error {
	if err := r.validateChannels(); err != nil {
		return err
	}
	if err := r.validateContinuity(); err != nil {
		return err
	}
	if err := r.validateConsent(); err != nil {
		return err
	}
	if err := r.validateDisambiguation(); err != nil {
		return err
	}
	if err := r.validatePrivacy(); err != nil {
		return err
	}
	return r.validateAudit()
}

func (r Record) validateChannels() error {
	kinds := make(map[string]bool, len(r.Channels))
	for _, channel := range r.Channels {
		if channel.ID == "" || channel.Name == "" || channel.Kind == "" {
			return errors.New("渠道资料不完整")
		}
		kinds[channel.Kind] = true
	}
	for _, required := range []string{"online_staff", "station_window", "offline_backfill"} {
		if !kinds[required] {
			return errors.New("缺少必要渠道: " + required)
		}
	}
	return nil
}

func (r Record) validateContinuity() error {
	if strings.TrimSpace(r.Continuity.Handover) == "" {
		return errors.New("缺少跨渠道接续说明")
	}
	if !r.Continuity.RequireConsent {
		return errors.New("跨渠道接续必须以本人同意为前提")
	}
	if !r.Continuity.SameRequestAcrossChannels {
		return errors.New("旅客必须能够跨渠道延续同一请求")
	}
	for _, required := range []string{
		CategoryDocumentVersion, CategoryVerification, CategoryTicketIdentity,
		CategoryTripLink, CategoryMultilingual, CategoryManualHandling,
	} {
		if !contains(r.Continuity.CarryOn, required) {
			return errors.New("接续资料缺少必要类别: " + required)
		}
	}
	return nil
}

func (r Record) validateConsent() error {
	if strings.TrimSpace(r.Consent.Holder) == "" {
		return errors.New("缺少同意持有人")
	}
	if !r.Consent.Revocable || strings.TrimSpace(r.Consent.OnRevocation) == "" {
		return errors.New("授权必须可撤回并说明撤回后果")
	}
	decisions := make(map[string]bool, len(r.Consent.Scopes))
	for _, scope := range r.Consent.Scopes {
		if scope.Category == "" || strings.TrimSpace(scope.Purpose) == "" {
			return errors.New("授权范围缺少类别或用途")
		}
		decisions[scope.Category] = scope.Allowed
	}
	for _, category := range []string{
		CategoryDocumentVersion, CategoryVerification, CategoryTicketIdentity,
		CategoryTripLink, CategoryMultilingual, CategoryManualHandling,
	} {
		if !decisions[category] {
			return errors.New("必要接续类别未获本人授权: " + category)
		}
	}
	if decisions[CategoryUnrelatedVisa] {
		return errors.New("不得复制与本次出行无关的签证资料")
	}
	if !contains(r.Privacy.ExcludedData, CategoryUnrelatedVisa) {
		return errors.New("隐私控制未排除与本次出行无关的签证资料")
	}
	return nil
}

func (r Record) validateDisambiguation() error {
	covered := make(map[string]bool, len(r.Disambiguation.Rules))
	for _, rule := range r.Disambiguation.Rules {
		if rule.Case == "" || strings.TrimSpace(rule.Policy) == "" {
			return errors.New("消歧规则缺少场景或处置策略")
		}
		if !strings.Contains(rule.Policy, "审慎") {
			return errors.New("场景 " + rule.Case + " 必须采用审慎规则")
		}
		if !rule.RequiresHuman {
			return errors.New("场景 " + rule.Case + " 必须保留人工确认")
		}
		covered[rule.Case] = true
	}
	for _, required := range []string{
		CaseNameSimilar, CaseTravelingFamily, CaseCrossBorderTrip, CaseOfflineWindowBackfill,
	} {
		if !covered[required] {
			return errors.New("消歧规则缺少审慎场景: " + required)
		}
	}
	if r.Disambiguation.ConfidenceFloor <= 0 || r.Disambiguation.ConfidenceFloor >= 1 {
		return errors.New("人工确认置信度门槛必须介于 0 与 1 之间")
	}
	if !r.Disambiguation.LowConfidenceToHuman {
		return errors.New("低置信度匹配必须交由人工确认")
	}
	return nil
}

func (r Record) validatePrivacy() error {
	if len(r.Privacy.StationView) == 0 {
		return errors.New("缺少车站窗口最小可见范围")
	}
	if strings.TrimSpace(r.Privacy.RetentionLimit) == "" {
		return errors.New("缺少资料保留期限")
	}
	if strings.TrimSpace(r.Privacy.AfterDocumentUpdate) == "" ||
		strings.TrimSpace(r.Privacy.AfterConsentRevoked) == "" {
		return errors.New("缺少证件更新或授权撤回后的最小可见说明")
	}
	for _, visible := range r.Privacy.StationView {
		for _, excluded := range r.Privacy.ExcludedData {
			if visible == excluded {
				return errors.New("车站可见范围包含已排除资料: " + visible)
			}
		}
	}
	if len(r.Privacy.ExcludedData) == 0 {
		return errors.New("缺少明确排除的资料类别")
	}
	return nil
}

func (r Record) validateAudit() error {
	for _, required := range []string{RiskWrongMerge, RiskUnauthorizedView, RiskOverRetention} {
		if !contains(r.Audit.DetectableRisks, required) {
			return errors.New("审计必须能够发现风险: " + required)
		}
	}
	if !contains(r.Audit.TraceableFrom, SourceRefundChange) {
		return errors.New("审计必须支持从退改结果发起回溯")
	}
	for _, required := range []string{TargetActualConsent, TargetVerificationLog} {
		if !contains(r.Audit.TraceTargets, required) {
			return errors.New("审计回溯缺少目标: " + required)
		}
	}
	return nil
}

func contains(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
