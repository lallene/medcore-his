package complete

import (
	"fmt"
	"strings"
)

// Fictitious identities (Côte d'Ivoire context of the product). Combinations are drawn from
// the seeded RNG; a small share of homonyms is produced on purpose (same name + first names).

var lastNames = []string{
	"KOUASSI", "KONE", "TRAORE", "YAO", "N'GUESSAN", "KOFFI", "OUATTARA", "BAMBA", "COULIBALY",
	"DIABATE", "TOURE", "DIALLO", "SANOGO", "KOUAME", "AKA", "ASSI", "GNAGNE", "BROU", "AMANI",
	"ZADI", "DAGO", "SERI", "TAPE", "LOBA", "ZOUZOU", "KACOU", "N'DRI", "AHOUA", "BLE", "YAPI",
	"ESSIS", "MEL", "AKE", "ANOH", "ADOU", "ATSE", "BOKA", "DJE", "EHUI", "GOLI", "GUEU", "KASSI",
	"KOUADIO", "KRAGBE", "LAGO", "N'GORAN", "NIAMKE", "OKOU", "SEKA", "SORO", "TANO", "TIA",
	"YEO", "ZAMBLE", "FOFANA", "CISSE", "DOUMBIA", "KAMARA", "SYLLA", "SIDIBE", "BAKAYOKO",
	"DIOMANDE", "KEITA", "MEITE", "DOSSO", "FADIGA", "GBANE", "KONATE", "SAVANE", "TIEMOKO",
}

var maleFirstNames = []string{
	"Emmanuel", "Jean", "Koffi", "Yao", "Kouamé", "Ibrahim", "Moussa", "Adama", "Seydou", "Mamadou",
	"Serge", "Hervé", "Olivier", "Didier", "Arnaud", "Christian", "Franck", "Guy", "Patrick", "Roland",
	"Ange", "Aristide", "Brice", "Cédric", "Daouda", "Eric", "Fabrice", "Gérard", "Habib", "Issa",
	"Jacques", "Kader", "Lassina", "Marcel", "Noël", "Ousmane", "Pacôme", "Romuald", "Souleymane", "Thierry",
	"Victor", "Wilfried", "Yannick", "Zié", "Kouadio Jean", "Yao Serge", "Konan", "Aboubakar", "Drissa", "Bakary",
}

var femaleFirstNames = []string{
	"Grâce", "Aya", "Adjoua", "Akissi", "Amenan", "Mariam", "Fatou", "Aminata", "Awa", "Salimata",
	"Christelle", "Nadège", "Josiane", "Sandrine", "Brigitte", "Carine", "Danielle", "Estelle", "Florence", "Gisèle",
	"Hélène", "Inès", "Jocelyne", "Karidja", "Laetitia", "Marie-Claire", "Nathalie", "Odile", "Patricia", "Rachelle",
	"Sabine", "Tatiana", "Viviane", "Yasmine", "Zénab", "Affoué", "Ahou", "Assita", "Bintou", "Kadidja",
	"Rokia", "Alimata", "Ange-Marie", "Délphine", "Eliane", "Flora", "Henriette", "Irène", "Juliette", "Murielle",
}

var districts = []string{
	"Cocody", "Yopougon", "Abobo", "Adjamé", "Plateau", "Treichville", "Marcory", "Koumassi",
	"Port-Bouët", "Attécoubé", "Bingerville", "Anyama", "Songon", "Riviera", "Angré", "Deux-Plateaux",
}

var relationships = []string{"Conjoint", "Mère", "Père", "Frère", "Sœur", "Fils", "Fille", "Oncle", "Tante", "Ami"}

type identity struct {
	Last, First, Sex string
}

func (id identity) Full() string { return id.First + " " + id.Last }

func newIdentity(g *RNG, sex string) identity {
	if sex == "" {
		sex = "F"
		if g.Chance(0.48) {
			sex = "M"
		}
	}
	first := Pick(g, femaleFirstNames)
	if sex == "M" {
		first = Pick(g, maleFirstNames)
	}
	if g.Chance(0.12) { // composed first names
		other := Pick(g, femaleFirstNames)
		if sex == "M" {
			other = Pick(g, maleFirstNames)
		}
		if other != first {
			first = first + " " + other
		}
	}
	return identity{Last: Pick(g, lastNames), First: first, Sex: sex}
}

// phone returns a unique Ivorian mobile number derived from a sequence number.
func phone(prefix string, n int) string {
	return fmt.Sprintf("+225 %s %02d %02d %02d %02d", prefix, (n/1000000)%100, (n/10000)%100, (n/100)%100, n%100)
}

func slug(s string) string {
	r := strings.NewReplacer("'", "", " ", ".", "é", "e", "è", "e", "ê", "e", "ë", "e", "ô", "o", "î", "i", "â", "a", "ç", "c", "œ", "oe")
	return strings.ToLower(r.Replace(s))
}
