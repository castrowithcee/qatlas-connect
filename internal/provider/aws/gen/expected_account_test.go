package gen

// Operations of the account curation tables that name a guard.

func init() {
	expectGuarded(
		"DisableRegion",
		"EnableRegion",
		"GetRegionOptStatus",
	)
}
