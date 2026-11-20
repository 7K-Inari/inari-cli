package extension

const (
	BackendContract = "inari.plugin.v1"
	UIHostAPI       = "inari.ui.v1"
	UISDKRange      = ">=0.1.8 <2.0.0"
)

// ContractIntersection reports whether the declared contract is advertised.
func ContractIntersection(declared string, supported []string) bool {
	for _, candidate := range supported {
		if candidate == declared {
			return true
		}
	}
	return false
}
