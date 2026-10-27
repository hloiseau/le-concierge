package config

import "testing"

func TestOuicheurRequiresAuditedSecureOrigin(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("PUBLIC_ORIGIN", "https://hub.test")
	t.Setenv("BOT_API_SECRET", "01234567890123456789012345678901")
	for _, tc := range []struct {
		url, revision string
		valid         bool
	}{
		{"", "", true}, {"https://gifts.test", "bbd143bc03201205a35b0a1009bf8476e110f16a", true},
		{"https://gifts.test", "", false}, {"https://gifts.test", "unknown", false},
		{"http://gifts.test", "bbd143bc03201205a35b0a1009bf8476e110f16a", false},
		{"https://a:secret@gifts.test", "bbd143bc03201205a35b0a1009bf8476e110f16a", false},
		{"https://gifts.test/path", "bbd143bc03201205a35b0a1009bf8476e110f16a", false},
	} {
		t.Run(tc.url+tc.revision, func(t *testing.T) {
			t.Setenv("OUICHEUR_URL", tc.url)
			t.Setenv("OUICHEUR_REVISION", tc.revision)
			_, err := Load()
			if (err == nil) != tc.valid {
				t.Fatal(tc, err)
			}
		})
	}
}
