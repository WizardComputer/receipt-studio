package i18n

import (
	"testing"
)

func TestTranslationsPreserveValuesAndLocalizeErrors(t *testing.T) {
	for source, translated := range english {
		if len(formatToken.FindAllString(source, -1)) != len(formatToken.FindAllString(translated, -1)) {
			t.Fatalf("format mismatch: %s", source)
		}
	}
	for _, test := range [][3]string{
		{"bs", "Poslovi štampe", "Poslovi printanja"},
		{"bs", "Poslano 774 bajtova. Fizička štampa nije potvrđena.", "Poslano 774 bajtova. Fizičko printanje nije potvrđeno."},
		{"en", "Poslano 774 bajtova. Fizička štampa nije potvrđena.", "Sent 774 bytes. Physical printing is not confirmed."},
		{"en", "Verzija 12 · trenutna", "Version 12 · current"},

		{"en", "Primjer podataka: Error: Nedostaje podatak: shop at fail (receipt-core.js:1:2)", "Sample data: Error: Missing data: shop at fail (receipt-core.js:1:2)"},
	} {
		if got := Translate(test[0], BosnianPrintTerms(test[1])); got != test[2] {
			t.Fatalf("%s: %q, want %q", test[1], got, test[2])
		}
	}
}

func TestTranslationKeepsQuotedUserName(t *testing.T) {
	if got := Translate("en", "Obrisati predložak „Prodaja štampe“?"); got != "Delete template “Prodaja štampe”?" {
		t.Fatal(got)
	}
}
