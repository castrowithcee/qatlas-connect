package googlemerchant

import "testing"

// at keeps user information in the cases apart from anything that reads like a mail address.
const at = "@"

func TestCheckHTTPSAddress(t *testing.T) {
	for _, ok := range []string{
		"https://shop.example.invalid",
		"https://shop.example.invalid/",
		"HTTPS://shop.example.invalid/returns?lang=en#policy",
		"https://shop.example.invalid:8443/path",
		"https://shop.example.invalid/checkout/{id}?offer={offer_id}",
		"https://[2001:db8::1]/",
	} {
		if err := checkHTTPSAddress("uri", ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, refused := range []string{
		"",
		"http://shop.example.invalid/",
		"ftp://shop.example.invalid/",
		"javascript:alert(1)",
		"https:shop.example.invalid",
		"https:///path",
		"//shop.example.invalid/",
		"shop.example.invalid",
		"https://user" + at + "shop.example.invalid/",
		"https://user:word" + at + "shop.example.invalid/",
		"https://shop.example.invalid\\@other.example.invalid/",
		"https://shop.example.invalid /",
		"https://shop.example.invalid/\nX",
		"https://shop.example.invalid/ ",
		"https://shop.example.invalid:port/",
		"https://{store}.example.invalid/",
	} {
		err := checkHTTPSAddress("uri", refused)
		if !isInvalidRequest(err) {
			t.Errorf("%q: err = %v, want an invalid request", refused, err)
		}
	}
}
