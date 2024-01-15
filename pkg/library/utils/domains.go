package utils

// ValidSubdomains returns (true, "") when every single subdomains item
// is a subset of at least one of the domains.
// Domains and subdomains may be prefixed with a wildcard label (*.).
// The wildcard label must appear by itself as the first label.
// When one of the subdomains is not a subset of the domains, it returns false and
// the subdomain not being subset of the domains
func ValidSubdomains(domains, subdomains []string) (bool, string) {
	for _, subdomain := range subdomains {
		validSubdomain := false
		for _, domain := range domains {
			if Name(subdomain).SubsetOf(Name(domain)) {
				validSubdomain = true
				break
			}
		}

		if !validSubdomain {
			return false, subdomain
		}
	}
	return true, ""
}
