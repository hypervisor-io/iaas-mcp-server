package tools

import "encoding/json"

// Pointer fields give the MCP schema optional nullable scalars. Presence is
// recorded separately because encoding/json decodes both absent and null as nil.
func (in *CreateInstanceBackupPolicyInput) UnmarshalJSON(data []byte) error {
	type plain CreateInstanceBackupPolicyInput
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	present, err := policyFieldPresence(data)
	if err != nil {
		return err
	}
	*in = CreateInstanceBackupPolicyInput(decoded)
	in.present = present
	return nil
}

func (in *UpdateInstanceBackupPolicyInput) UnmarshalJSON(data []byte) error {
	type plain UpdateInstanceBackupPolicyInput
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	present, err := policyFieldPresence(data)
	if err != nil {
		return err
	}
	*in = UpdateInstanceBackupPolicyInput(decoded)
	in.present = present
	return nil
}

func policyFieldPresence(data []byte) (map[string]bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(fields))
	for key := range fields {
		present[key] = true
	}
	return present, nil
}

func putPolicyField[T any](body map[string]any, present map[string]bool, key string, value *T) {
	if value != nil {
		body[key] = *value
	} else if present[key] {
		body[key] = nil
	}
}

func addInstancePolicyRetention(body map[string]any, present map[string]bool, mode, zone *string, count, last, within, daily, weekly, monthly, yearly *int) {
	putPolicyField(body, present, "retention_mode", mode)
	putPolicyField(body, present, "timezone", zone)
	for key, value := range map[string]*int{
		"retention_count": count, "keep_last": last, "keep_within_days": within,
		"keep_daily": daily, "keep_weekly": weekly, "keep_monthly": monthly, "keep_yearly": yearly,
	} {
		putPolicyField(body, present, key, value)
	}
}
