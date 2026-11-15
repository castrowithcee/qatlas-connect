package aws

// Tools of the account tables that name a guard.

func init() {
	expectGuarded(
		"aws.account.region.disable",
		"aws.account.region.enable",
		"aws.account.region.get-opt-status",
	)
}
