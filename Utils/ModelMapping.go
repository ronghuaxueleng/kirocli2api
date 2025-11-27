package Utils

func ModelMapping(model string) string {
	if model == "claude-sonnet-4-5-20250929" || model == "claude-haiku-4-5-20251001" {
		model = "claude-sonnet-4.5"
	} else if model == "claude-sonnet-4-20250514" {
		model = "claude-sonnet-4"
	} else if model == "claude-3.5-sonnet-20241022" || model == "claude-3.5-sonnet-20240620" || model == "claude-3-5-sonnet-20241022" || model == "claude-3-5-sonnet-20240620" || model == "claude-3-5-haiku-20241022" {
		model = "claude-3.5-sonnet"
	}

	return model
}
