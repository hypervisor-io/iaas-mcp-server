package tools

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Sandbox outbound network policy tools. A sandbox (microVM) can carry a
// policy that decides what it may connect to: a default action (allow or
// deny) plus rules by host name, wildcard, address or network, protocol and
// ports. No policy means unrestricted outbound access.
//
//	user.microvm.egress.get    GET    /microvm/vm/{id}/egress
//	user.microvm.egress.set    PUT    /microvm/vm/{id}/egress  (whole-document replace)
//	user.microvm.egress.clear  DELETE /microvm/vm/{id}/egress  (confirm-gated)
//	user.microvm.egress.log    GET    /microvm/vm/{id}/egress/log  (what the policy refused)
//	user.microvm.egress.presets GET   /microvm/egress/presets      (ready-made policies)
//
// A write is stored with a new generation and sent to the sandbox's node in
// the background; the result's state says how far the node is (pending until
// it confirms). user.microvm.create takes the same document as egress_policy,
// so a sandbox can start with its policy already in force.

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmEgressTools)
}

// MicrovmEgressRuleInput is one rule of an outbound policy.
type MicrovmEgressRuleInput struct {
	Action   string   `json:"action" jsonschema:"allow or deny. A deny rule wins over an allow rule for the same destination"`
	Target   string   `json:"target" jsonschema:"where the rule applies: a host name (api.github.com), a wildcard (*.example.com, which does not match example.com itself), an IPv4 or IPv6 address, or a network in CIDR form"`
	Protocol string   `json:"protocol,omitempty" jsonschema:"tcp, udp, icmp or any; omitted means any"`
	Ports    []string `json:"ports,omitempty" jsonschema:"destination ports as strings: a port (\"443\") or, on address and network rules only, a range (\"8000-8100\"); omitted means every port. A host name rule takes at most 8 ports"`
	Comment  string   `json:"comment,omitempty" jsonschema:"a note, at most 120 characters"`
	ID       string   `json:"id,omitempty" jsonschema:"optional rule id (1 to 32 letters, digits, underscore, dot, hyphen), unique within the policy; assigned by the platform when omitted"`
	Source   string   `json:"source,omitempty" jsonschema:"where the rule came from, at most 64 characters; a rule taken from user.microvm.egress.presets carries preset:<id>@<version>. No effect on enforcement"`
}

// MicrovmEgressPolicyInput is the policy document: the body of the replace
// call and the egress_policy field of user.microvm.create.
type MicrovmEgressPolicyInput struct {
	DefaultAction  string                   `json:"default_action" jsonschema:"what happens to outbound traffic no rule matches: allow or deny"`
	InboundReplies string                   `json:"inbound_replies,omitempty" jsonschema:"which replies to inbound connections may leave the sandbox: service_ports (only the ports listed as service_ports on the read) or any. Omit to let the platform choose (service_ports with deny, any with allow); any is refused together with deny"`
	Rules          []MicrovmEgressRuleInput `json:"rules,omitempty" jsonschema:"the rules, matched before the default action, at most 100. Omit or leave empty for a policy that is the default action alone"`
}

type GetMicrovmEgressInput struct {
	ID string `json:"id" jsonschema:"UUID of the sandbox (microVM)"`
}

// SetMicrovmEgressInput replaces the whole policy of one sandbox.
type SetMicrovmEgressInput struct {
	ID string `json:"id" jsonschema:"UUID of the sandbox (microVM)"`
	MicrovmEgressPolicyInput
}

// ClearMicrovmEgressInput removes the policy. Confirm-gated: the sandbox goes
// back to unrestricted outbound access.
type ClearMicrovmEgressInput struct {
	ID string `json:"id" jsonschema:"UUID of the sandbox (microVM) whose outbound policy is removed"`
	Confirmation
}

// MicrovmEgressResult is the platform's read model of a sandbox's policy.
type MicrovmEgressResult struct {
	// Egress holds policy (null when none is set), generation,
	// applied_generation, state (none, pending, applied, failed, unavailable,
	// or inactive for a killed or errored sandbox whose policy is kept for
	// reading), applied_at, last_error_code, capability and service_ports.
	Egress map[string]any `json:"egress"`
	// Confirmed is true when the node enforces the stored generation (state
	// applied, or none after a removal). False means the change is stored and
	// the node has not confirmed it yet, refused it, or cannot enforce it.
	Confirmed bool `json:"confirmed"`
}

// egressPolicyBody converts the typed input into the policy document the API
// takes. Only what the caller set is sent; the platform applies its defaults
// and its own validation (the refusal names the field and the reason).
func egressPolicyBody(in MicrovmEgressPolicyInput) map[string]any {
	body := map[string]any{"default_action": in.DefaultAction}
	if in.InboundReplies != "" {
		body["inbound_replies"] = in.InboundReplies
	}
	rules := make([]map[string]any, 0, len(in.Rules))
	for _, r := range in.Rules {
		rule := map[string]any{"action": r.Action, "target": r.Target}
		if r.Protocol != "" {
			rule["protocol"] = r.Protocol
		}
		if len(r.Ports) > 0 {
			ports := make([]any, 0, len(r.Ports))
			for _, p := range r.Ports {
				p = strings.TrimSpace(p)
				// A single port travels as a number and a range as "lo-hi", the API's own shapes.
				if n, err := strconv.Atoi(p); err == nil && n >= 0 {
					ports = append(ports, n)
				} else {
					ports = append(ports, p)
				}
			}
			rule["ports"] = ports
		}
		if r.Comment != "" {
			rule["comment"] = r.Comment
		}
		if r.ID != "" {
			rule["id"] = r.ID
		}
		if r.Source != "" {
			rule["source"] = r.Source
		}
		rules = append(rules, rule)
	}
	body["rules"] = rules
	return body
}

func egressResult(read map[string]any) MicrovmEgressResult {
	state, _ := read["state"].(string)
	generation, _ := read["generation"].(float64)
	applied, _ := read["applied_generation"].(float64)
	return MicrovmEgressResult{
		Egress:    read,
		Confirmed: (state == "applied" || state == "none") && applied >= generation,
	}
}

func getMicrovmEgress(ctx context.Context, cl *client.Client, in GetMicrovmEgressInput) (MicrovmEgressResult, error) {
	read, err := cl.GetMicrovmEgress(ctx, in.ID)
	if err != nil {
		return MicrovmEgressResult{}, err
	}
	return egressResult(read), nil
}

func setMicrovmEgress(ctx context.Context, cl *client.Client, in SetMicrovmEgressInput) (MicrovmEgressResult, error) {
	read, err := cl.SetMicrovmEgress(ctx, in.ID, egressPolicyBody(in.MicrovmEgressPolicyInput))
	if err != nil {
		return MicrovmEgressResult{}, err
	}
	return egressResult(read), nil
}

func clearMicrovmEgress(ctx context.Context, cl *client.Client, in ClearMicrovmEgressInput) (MicrovmEgressResult, error) {
	read, err := cl.ClearMicrovmEgress(ctx, in.ID)
	if err != nil {
		return MicrovmEgressResult{}, err
	}
	return egressResult(read), nil
}

// MicrovmEgressLogInput reads one page of a sandbox's refusal log.
type MicrovmEgressLogInput struct {
	ID      string `json:"id" jsonschema:"UUID of the sandbox (microVM)"`
	Layer   string `json:"layer,omitempty" jsonschema:"only dns (refused host name lookups) or only packet (dropped connections)"`
	Class   string `json:"class,omitempty" jsonschema:"only entries of this class: default, rule, floor, private, dns_name_denied, dns_cname_denied, dns_floor_stripped, dns_set_full, dns_malformed or suppressed"`
	RuleID  string `json:"rule_id,omitempty" jsonschema:"only entries decided by this rule id"`
	Search  string `json:"search,omitempty" jsonschema:"only entries whose destination contains this text"`
	Since   string `json:"since,omitempty" jsonschema:"only entries last seen at or after this time (ISO 8601, or Unix seconds)"`
	Until   string `json:"until,omitempty" jsonschema:"only entries last seen at or before this time (ISO 8601, or Unix seconds)"`
	Page    string `json:"page,omitempty" jsonschema:"1-based page number"`
	PerPage string `json:"per_page,omitempty" jsonschema:"entries per page, 50 by default, at most 100"`
}

// MicrovmEgressLogResult is one page of refusals, newest first.
type MicrovmEgressLogResult struct {
	// Events are aggregates: layer (dns or packet), class, rule (a rule id,
	// default, or floor:<category>), destination (a host name or an address;
	// null with withheld true for a platform network), protocol, port, qtype,
	// count, sampled, first_at, last_at.
	Events []map[string]any `json:"events"`
	// Total is how many entries match the filters; Page, PerPage and LastPage
	// say where this page sits.
	Total    int `json:"total"`
	Page     int `json:"page"`
	PerPage  int `json:"per_page"`
	LastPage int `json:"last_page"`
	// Collection says when the platform last collected the log from the node
	// successfully (pulled_at; the collection made right before a stop, kill or
	// delete counts), whether the node records packet entries (packet_layer) and
	// how long entries are kept (retention_days, max_rows).
	Collection map[string]any `json:"collection"`
}

func microvmEgressLog(ctx context.Context, cl *client.Client, in MicrovmEgressLogInput) (MicrovmEgressLogResult, error) {
	answer, err := cl.GetMicrovmEgressLog(ctx, in.ID, map[string]string{
		"layer": in.Layer, "class": in.Class, "rule_id": in.RuleID, "search": in.Search,
		"since": in.Since, "until": in.Until, "page": in.Page, "per_page": in.PerPage,
	})
	if err != nil {
		return MicrovmEgressLogResult{}, err
	}
	page, _ := answer["events"].(map[string]any)
	rows, _ := page["data"].([]any)
	out := MicrovmEgressLogResult{Events: make([]map[string]any, 0, len(rows))}
	for _, row := range rows {
		if event, ok := row.(map[string]any); ok {
			out.Events = append(out.Events, event)
		}
	}
	number := func(key string) int {
		n, _ := page[key].(float64)
		return int(n)
	}
	out.Total, out.Page, out.PerPage, out.LastPage = number("total"), number("current_page"), number("per_page"), number("last_page")
	out.Collection, _ = answer["collection"].(map[string]any)
	return out, nil
}

// MicrovmEgressPresetsInput has no fields: the catalog is the same for every
// sandbox of the account.
type MicrovmEgressPresetsInput struct{}

// MicrovmEgressPresetsResult lists the ready-made policies.
type MicrovmEgressPresetsResult struct {
	// Presets each carry id, version, source (preset:<id>@<version>), name,
	// description, rule_count and policy: a complete document for
	// user.microvm.egress.set or for egress_policy on user.microvm.create.
	Presets []map[string]any `json:"presets"`
}

func listMicrovmEgressPresets(ctx context.Context, cl *client.Client, _ MicrovmEgressPresetsInput) (MicrovmEgressPresetsResult, error) {
	presets, err := cl.ListMicrovmEgressPresets(ctx)
	if err != nil {
		return MicrovmEgressPresetsResult{}, err
	}
	return MicrovmEgressPresetsResult{Presets: presets}, nil
}

func registerMicrovmEgressTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.egress.presets", Description: "List the ready-made outbound network policies for sandboxes (for example package registries only, AI provider APIs only, no outbound access). Each preset carries a complete policy document: pass its default_action and rules to user.microvm.egress.set, or as egress_policy to user.microvm.create, as returned or after editing (rules of several presets can be combined into one policy, and your own rules added). Applying a preset stores an ordinary policy: the sandbox does not stay linked to the preset and a later change of this list never changes a stored policy. Every preset rule carries source preset:<id>@<version>; keep it when you send the rule so a stored rule shows where it came from. All presets block by default and allow their destinations on tcp 443 only. Never changes anything."}, listMicrovmEgressPresets)
	Register(s, deps, Spec{Name: "user.microvm.egress.log", Description: "List what a sandbox's outbound network policy refused, newest first: refused host name lookups (layer dns) and dropped connections (layer packet), each an aggregate with a count between first_at and last_at. rule says what decided: a rule id, default (no rule allowed it), or floor:<category> (a destination the platform always blocks). An entry with withheld true hit one of the platform's own networks and carries no destination. Use it to find out why something inside a sandbox cannot connect, then add an allow rule with user.microvm.egress.set. Only refusals are recorded, never allowed traffic, packet contents, URLs or DNS answers. The log is collected from the sandbox's node about once a minute, kept for a limited time (see collection) and stays readable after the sandbox is killed; it is empty for a sandbox that never had a policy. Filter with layer, class, rule_id, search, since and until; page with page and per_page (at most 100). Never changes anything."}, microvmEgressLog)
	Register(s, deps, Spec{Name: "user.microvm.egress.get", Description: "Read a sandbox's outbound network policy: the rules (policy is null when none is set, meaning unrestricted outbound access), its generation, and how far the node has applied it (state none, pending, applied, failed or unavailable, with last_error_code when the node refused it; inactive when the sandbox is killed or errored: the stored policy is kept for reading and is in force nowhere). Also lists service_ports, the sandbox ports that keep answering inbound connections under a default deny policy. Never changes anything."}, getMicrovmEgress)
	Register(s, deps, Spec{Name: "user.microvm.egress.set", Description: "Replace a sandbox's whole outbound network policy: default_action (allow or deny) plus rules by host name, wildcard, address or network, protocol and ports. This is a full replace, not a merge: read the current policy first and send every rule that should remain. The platform always blocks some destinations whatever the policy says (metadata and link-local addresses, loopback, private networks other than the sandbox's own VPC subnets, the platform's own addresses, tcp 25 and tcp 853) and refuses an allow rule that could only match those. The policy is stored and sent to the node in the background; confirmed is false until the node enforces it (a stopped sandbox starts with it). Refused with a typed reason for a bad policy (the error names the field), a killed sandbox, or a node that cannot enforce policies. Traffic of the tools gateway and of kit builds is not covered."}, setMicrovmEgress)
	Register(s, deps, Spec{Name: "user.microvm.egress.clear", Description: "Remove a sandbox's outbound network policy, returning it to unrestricted outbound access (security groups still apply). This is not the same as a policy with default_action allow, which still applies its deny rules. Removing a policy that is not set changes nothing. DESTRUCTIVE (it lifts a network restriction): requires \"confirm\": true.", Destructive: true}, clearMicrovmEgress)
}
