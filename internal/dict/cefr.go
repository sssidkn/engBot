package dict

import "strings"

// EstimateCEFR is a coarse fallback when the LLM is off.
func EstimateCEFR(word string) (level, why string) {
	w := strings.ToLower(strings.TrimSpace(word))
	w = strings.Trim(w, ".,!?;:'\"")
	if w == "" {
		return "", ""
	}
	if lv, ok := cefrExact[w]; ok {
		return lv, cefrWhy[lv]
	}
	for _, p := range strings.Fields(w) {
		if lv, ok := cefrExact[p]; ok && len(strings.Fields(w)) == 1 {
			return lv, cefrWhy[lv]
		}
	}
	letters := 0
	for _, r := range w {
		if r >= 'a' && r <= 'z' {
			letters++
		}
	}
	switch {
	case letters <= 3:
		return "A1", cefrWhy["A1"]
	case letters <= 5:
		return "A2", cefrWhy["A2"]
	case strings.Contains(w, "tion") || strings.Contains(w, "sion") || strings.Contains(w, "ment"):
		return "B2", cefrWhy["B2"]
	case letters >= 12:
		return "C1", cefrWhy["C1"]
	default:
		return "B1", cefrWhy["B1"]
	}
}

var cefrWhy = map[string]string{
	"A1": "Базовая лексика первых учебников.",
	"A2": "Частое слово уровня elementary / pre-intermediate.",
	"B1": "Нужно для повседневного общения на intermediate.",
	"B2": "Более точная или абстрактная лексика upper-intermediate.",
	"C1": "Продвинутая лексика, чаще в текстах и дискуссиях.",
	"C2": "Редкое или стилистически сложное употребление.",
}

var cefrExact = map[string]string{
	"a": "A1", "the": "A1", "i": "A1", "you": "A1", "he": "A1", "she": "A1", "it": "A1",
	"we": "A1", "they": "A1", "be": "A1", "is": "A1", "are": "A1", "am": "A1", "have": "A1",
	"do": "A1", "go": "A1", "get": "A1", "make": "A1", "see": "A1", "know": "A1", "come": "A1",
	"think": "A1", "look": "A1", "want": "A1", "give": "A1", "use": "A1", "find": "A1",
	"tell": "A1", "ask": "A1", "work": "A1", "seem": "A1", "feel": "A1", "try": "A1",
	"leave": "A1", "call": "A1", "good": "A1", "new": "A1", "first": "A1", "last": "A1",
	"long": "A1", "great": "A1", "little": "A1", "own": "A1", "other": "A1", "old": "A1",
	"right": "A1", "big": "A1", "high": "A1", "different": "A2", "small": "A1", "large": "A2",
	"next": "A1", "early": "A1", "young": "A1", "important": "A2", "few": "A1", "public": "A2",
	"bad": "A1", "same": "A1", "able": "A2", "time": "A1", "person": "A1", "year": "A1",
	"way": "A1", "day": "A1", "thing": "A1", "man": "A1", "world": "A1", "life": "A1",
	"hand": "A1", "part": "A1", "child": "A1", "eye": "A1", "woman": "A1", "place": "A1",
	"week": "A1", "case": "A2", "point": "A2", "government": "B1", "company": "A2",
	"number": "A1", "group": "A1", "problem": "A2", "fact": "A2", "hello": "A1", "cat": "A1",
	"dog": "A1", "house": "A1", "run": "A1", "book": "A1", "water": "A1", "food": "A1",
	"however": "B1", "although": "B1", "therefore": "B2", "moreover": "B2",
	"significant": "B2", "consequence": "B2", "establish": "B2", "indicate": "B2",
	"assume": "B2", "constitute": "C1", "notwithstanding": "C1", "ubiquitous": "C1",
	"look after": "A2", "give up": "A2", "put off": "B1", "come across": "B2",
	"in spite of": "B1", "as soon as": "A2",
}
