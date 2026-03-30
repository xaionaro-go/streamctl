//go:build integration

package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testOllamaURLChain = "http://192.168.0.171:11434"

func newTestChain(t *testing.T) *TranslatorChain {
	t.Helper()
	return NewTranslatorChain("English", 20, []ProviderEntry{{
		Provider:    &OllamaProvider{APIURL: testOllamaURLChain, Model: "qwen3:30b-instruct"},
		Parallelism: 1,
	}})
}

func TestTranslate(t *testing.T) {
	type testCase struct {
		name     string
		user     string
		input    string
		wantSame bool              // expect output == input (English, emoji-only)
		contains []string          // output must contain all of these (case-insensitive)
		notEqual bool              // output must differ from input
		check    func(t *testing.T, input, output string) // custom check
	}

	cases := []testCase{
		{
			name:     "Spanish",
			user:     "IvanMartines",
			input:    "ola mi amor estás muy linda 😍",
			contains: []string{"😍"},
			notEqual: true,
		},
		{
			name:     "Turkish",
			user:     "Zafer",
			input:    "merhaba arkadaşım",
			contains: []string{"hello"},
			notEqual: true,
		},
		{
			name:     "Turkish/cold",
			user:     "Zafer",
			input:    "orası çok soğuk",
			contains: []string{"cold"},
			notEqual: true,
		},
		{
			name:     "Mixed Turkish+English",
			user:     "Zekeriya",
			input:    "Hiiiiiiiiii Aşkim Aşkim",
			notEqual: true,
			contains: []string{"love"},
		},
		{
			name: "Hindi meaning not transliteration",
			user: "tushar",
			input: "नमस्कार ☺️",
			contains: []string{"hello", "☺️"},
			check: func(t *testing.T, _, output string) {
				assert.NotContains(t, output, "Namaste", "should translate meaning, not transliterate")
			},
		},
		{
			name:     "Turkish with typo",
			user:     "Zafer",
			input:    "içecek neiçirirsin?",
			notEqual: true,
			contains: []string{"drink"},
			check: func(t *testing.T, _, output string) {
				assert.NotContains(t, strings.ToLower(output), "not drink",
					"should not misinterpret typo as negation")
			},
		},
		{
			name:     "Portuguese",
			user:     "Adelson",
			input:    "Oi Belém do Pará Brasil norte Amazônia",
			notEqual: true,
		},
		{
			name:     "Indonesian",
			user:     "agam",
			input:    "halo",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasGreeting := strings.Contains(lower, "hello") ||
					strings.Contains(lower, "hi")
				assert.True(t, hasGreeting,
					"Indonesian 'halo' should be translated to hello/hi: got %q", output)
			},
		},
		{
			name:     "English unchanged",
			user:     "Daniel",
			input:    "Hi How r U today good morning",
			wantSame: true,
		},
		{
			name:     "English with typos unchanged",
			user:     "Faouzi",
			input:    "Hello Vickey. faouzi fom Tunisia",
			wantSame: true,
		},
		{
			name:     "English informal unchanged",
			user:     "Malaya",
			input:    "YT having problem with the livestream viewers count section if u are seeing less viewers just bear with it for now🤣",
			wantSame: true,
		},
		{
			name:     "Emoji only",
			user:     "user",
			input:    "🎉",
			wantSame: true,
		},
		{
			name:     "Emoji only angry",
			user:     "Zafer",
			input:    "😡",
			wantSame: true,
		},

		// --- Unnecessary translation: English modified when it should be unchanged ---
		{
			name:     "English typo yr/away unchanged",
			user:     "Lyndseysophia83",
			input:    "Your very beautiful in yr own away",
			wantSame: true,
		},
		{
			name:     "English U abbreviation unchanged",
			user:     "Lyndseysophia83",
			input:    "U see if cooked I would clean",
			wantSame: true,
		},
		{
			name:     "English typo duing unchanged",
			user:     "asmadiilyas",
			input:    "what are you duing now my darling?",
			wantSame: true,
		},
		{
			name:     "English typo spieck unchanged",
			user:     "asmadiilyas",
			input:    "can you spieck Indoneia",
			wantSame: true,
		},
		{
			name:     "English lowercase i unchanged",
			user:     "kristellephone",
			input:    "Okay well Happy Monday i hope you have a nice rest of the day and yeah bye i gotta go",
			wantSame: true,
		},
		{
			name:     "English lowercase i miss unchanged",
			user:     "LJ-Jordan",
			input:    "I'm good.. i miss you malishka",
			wantSame: true,
		},
		{
			name:     "English l as I unchanged",
			user:     "AhmedMgz",
			input:    "l believe you",
			wantSame: true,
		},
		{
			name:     "English typo vivrator unchanged",
			user:     "DavidMedina",
			input:    "you need a vivrator then, release some of that build up anger....",
			wantSame: true,
		},
		{
			name:     "English typo somene unchanged",
			user:     "mirseferbagirov",
			input:    "maybe somene will hear my name,did you understand?",
			wantSame: true,
		},
		{
			name:     "English question rephrasing unchanged",
			user:     "Guy",
			input:    "You have Bar BQ sauce?",
			wantSame: true,
		},
		{
			name:     "English grammar fix I'm pass unchanged",
			user:     "PARSHURAM",
			input:    "I'm pass in 9th standard",
			wantSame: true,
		},
		{
			name:     "English typo fimaly unchanged",
			user:     "manishshivhare",
			input:    "Indian fimaly so beautiful",
			wantSame: true,
		},
		{
			name:     "English bot message Join to unchanged",
			user:     "BotRix",
			input:    "Join to my Discord https://discord.com/invite/AmSFeN5gNn !",
			wantSame: true,
		},
		{
			name:     "English bot message well pizza unchanged",
			user:     "BotRix",
			input:    "well pizza and burger is kinda the same both have bread and both have tomato sauce both have olives and both have veggies so pizza is the Italian burger",
			wantSame: true,
		},
		{
			name:     "English typo Europ Nato unchanged",
			user:     "dionid2792",
			input:    "Europ and Nato USA",
			wantSame: true,
		},
		{
			name:     "English Pipell is People not Pill",
			user:     "dionid2792",
			input:    "A Love Pipell From Ukraina",
			wantSame: true,
		},
		{
			name:     "English capitalization only unchanged",
			user:     "manishshivhare",
			input:    "Indian gwalior mp so beautiful",
			wantSame: true,
		},

		// --- Mistranslation: wrong meaning ---
		{
			name:     "Turkish açıktım means hungry not open",
			user:     "Zafer",
			input:    "sizi izlerken açıktım",
			notEqual: true,
			contains: []string{"hungry"},
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				assert.NotContains(t, lower, "open",
					"açıktım means 'I was hungry' not 'I was open'")
			},
		},
		{
			name:     "Turkish misafir geleçeğim must be translated",
			user:     "Zafer",
			input:    "misafir geleçeğim",
			notEqual: true,
			contains: []string{"guest"},
		},
		{
			name:     "Turkish yorulma means don't get tired",
			user:     "Zafer",
			input:    "yorulma",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				assert.Contains(t, lower, "don't",
					"yorulma is imperative negative: 'don't get tired'")
				assert.NotEqual(t, "get tired", lower,
					"must not drop the negative")
			},
		},
		{
			name:     "Turkish küserim means sulk not angry",
			user:     "Zafer",
			input:    "sizden küserim",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				assert.NotContains(t, lower, "angry",
					"küsmek means to sulk/be offended, not to get angry")
			},
		},
		{
			name:     "Turkish kullağımı means my ears not my peace",
			user:     "Zafer",
			input:    "müziğin sesi kullağımı rahatsız etti.",
			notEqual: true,
			contains: []string{"ear"},
			check: func(t *testing.T, _, output string) {
				assert.NotContains(t, strings.ToLower(output), "peace",
					"kullağımı means my ears, not my peace")
			},
		},
		{
			name:     "Turkish sevginizi katmak means put love into food",
			user:     "Zafer",
			input:    "siz yemeklere sevginizi katıyormusunuz?",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				assert.Contains(t, lower, "love",
					"must mention love")
				hasFoodWord := strings.Contains(lower, "food") ||
					strings.Contains(lower, "meal") ||
					strings.Contains(lower, "dish") ||
					strings.Contains(lower, "cook")
				assert.True(t, hasFoodWord,
					"must mention food/meals/cooking: got %q", output)
				assert.NotEqual(t, strings.ToLower("Do you have a love for food?"), lower,
					"wrong meaning: it asks about putting love INTO food while cooking")
			},
		},
		{
			name:     "Turkish öğretteyim means shall I teach not learning",
			user:     "Zafer",
			input:    "Türkçe öğretteyim ollur mu?",
			notEqual: true,
			contains: []string{"teach"},
			check: func(t *testing.T, _, output string) {
				assert.NotContains(t, strings.ToLower(output), "learning",
					"öğretteyim means 'shall I teach', not 'are you learning'")
			},
		},
		{
			name:     "Turkish benden sıkıldıysan means bored of me not arm tired",
			user:     "Zafer",
			input:    "benden sıkıldıysan Youtube çıkayım",
			notEqual: true,
			contains: []string{"bored"},
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				assert.NotContains(t, lower, "arm",
					"should not mention arm")
				assert.Contains(t, lower, "youtube",
					"must mention YouTube")
			},
		},
		{
			name:     "Turkish o means she/he not I",
			user:     "Zafer",
			input:    "o hep hazır yiyecekler",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				isThirdPerson := strings.Contains(lower, "she") ||
					strings.Contains(lower, "he") ||
					strings.Contains(lower, "they") ||
					strings.Contains(lower, "those") ||
					strings.Contains(lower, "that") ||
					strings.Contains(lower, "it")
				assert.True(t, isThirdPerson,
					"'o' is third person (she/he/it/those), not 'I': got %q", output)
			},
		},
		{
			name:     "Russian Бонжур епта no added content",
			user:     "Koorush-2",
			input:    "Бонжур епта 😄💐🐻✌",
			notEqual: true,
			contains: []string{"😄", "💐", "🐻", "✌"},
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				assert.NotContains(t, lower, "my love",
					"should not add 'my love' — епта is a filler/slang, not an endearment")
			},
		},
		{
			name:     "Phonetic English cen ai sey sllava must be translated",
			user:     "dionid2792",
			input:    "cen ai sey sllava Ukraina",
			notEqual: true,
			check: func(t *testing.T, input, output string) {
				assert.NotEqual(t, strings.ToLower(input), strings.ToLower(output),
					"must not just change capitalization — should interpret phonetic English")
			},
		},
		{
			name:     "Phonetic English mek naic Famili must be translated",
			user:     "dionid2792",
			input:    "mek naic Famili",
			notEqual: true,
			check: func(t *testing.T, input, output string) {
				assert.NotEqual(t, strings.ToLower(input), strings.ToLower(output),
					"must not just change capitalization — should interpret as 'make nice family'")
			},
		},

		// --- Missed translations: non-English injected without any translation ---
		{
			name:     "French bonsoir must be translated",
			user:     "KAMEL",
			input:    "bonsoir vickey",
			notEqual: true,
			contains: []string{"evening"},
		},
		{
			name:     "French vous êtes must be translated",
			user:     "KAMEL",
			input:    "vous êtes en Russie",
			notEqual: true,
			contains: []string{"russia"},
		},
		{
			name:     "Indonesian apa kabar must be translated",
			user:     "DewaJon",
			input:    "hy...sayang ...apa kabar...",
			notEqual: true,
		},
		{
			name:     "Indonesian nambah cantik must be translated",
			user:     "DewaJon",
			input:    "nambah cantik...",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasMeaning := strings.Contains(lower, "beauti") ||
					strings.Contains(lower, "pretti")
				assert.True(t, hasMeaning,
					"nambah cantik means 'getting prettier/more beautiful': got %q", output)
			},
		},
		{
			name:     "Indonesian udah makan must be translated",
			user:     "DewaJon",
			input:    "udah makan blm.sayang...",
			notEqual: true,
			contains: []string{"eat"},
		},
		{
			name:     "Indonesian lagi masak must be translated",
			user:     "DewaJon",
			input:    "lagi masak ap vickey ..",
			notEqual: true,
			contains: []string{"cook"},
		},
		{
			name:     "Turkish komutanım must be translated",
			user:     "Zafer",
			input:    "komutanım🫡",
			notEqual: true,
			contains: []string{"🫡"},
		},
		{
			name:     "Turkish pişmiş must be translated",
			user:     "Zafer",
			input:    "pişmiş",
			notEqual: true,
			contains: []string{"cook"},
		},
		{
			name:     "Hindi-English mix nhi aata must be translated",
			user:     "manishshivhare",
			input:    "no English language nhi aata h",
			notEqual: true,
		},
		{
			name:     "Indonesian untranslated vickey dah makan",
			user:     "DewaJon",
			input:    "vickey dah ..makan blm...",
			notEqual: true,
			contains: []string{"eat"},
		},
		{
			name:     "Indonesian JM brpa must be translated",
			user:     "DewaJon",
			input:    "JM brpa vickey ..",
			notEqual: true,
		},

		// --- Missed translations: Turkish messages injected without translation ---
		{
			name:     "Turkish standalone merhaba",
			user:     "Zafer",
			input:    "merhaba",
			notEqual: true,
			contains: []string{"hello"},
		},
		{
			name:     "Turkish Aşkim with city name",
			user:     "Zekeriya",
			input:    "Aşkim Aşkim istanbul",
			notEqual: true,
			contains: []string{"istanbul"},
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasTerm := strings.Contains(lower, "love") ||
					strings.Contains(lower, "darling") ||
					strings.Contains(lower, "dear")
				assert.True(t, hasTerm,
					"Aşkim is a Turkish endearment, must translate: got %q", output)
			},
		},
		{
			name:     "Turkish çok güzelsın with endearment",
			user:     "Zekeriya",
			input:    "çok güzelsın",
			notEqual: true,
			contains: []string{"beauti"},
		},
		{
			name:     "Turkish yiyeceklerin hepsi hazır",
			user:     "Zafer",
			input:    "yiyeceklerin hepsi hazır",
			notEqual: true,
			contains: []string{"food", "ready"},
		},
		{
			name:     "Turkish vatsap mixed with endearment",
			user:     "Zekeriya",
			input:    "Aşkim vatsap İstanbul",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasEndearment := strings.Contains(lower, "love") ||
					strings.Contains(lower, "darling") ||
					strings.Contains(lower, "dear")
				assert.True(t, hasEndearment,
					"Aşkim must be translated: got %q", output)
				assert.Contains(t, lower, "istanbul",
					"city name must be preserved")
			},
		},
		{
			name:     "Turkish come İstanbul mixed",
			user:     "Zekeriya",
			input:    "come İstanbul",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				assert.Contains(t, lower, "istanbul",
					"city name must be preserved")
				assert.Contains(t, lower, "come",
					"English part must be preserved")
			},
		},

		// --- Missed translations: Indonesian messages ---
		{
			name:     "Indonesian masak ikan kambing",
			user:     "DewaJon",
			input:    "dewa juga lagi masak ikan kambing",
			notEqual: true,
			contains: []string{"cook"},
		},
		{
			name:     "Indonesian nyuci lagi",
			user:     "DewaJon",
			input:    "nyuci lagi",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasWash := strings.Contains(lower, "wash") ||
					strings.Contains(lower, "clean") ||
					strings.Contains(lower, "laundry")
				assert.True(t, hasWash,
					"nyuci means washing/cleaning: got %q", output)
			},
		},

		// --- Missed translations: French ---
		{
			name:     "French comment allez-vous must be translated",
			user:     "KAMEL",
			input:    "comment allez-vous vickey",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasGreeting := strings.Contains(lower, "how are you") ||
					strings.Contains(lower, "how do you do")
				assert.True(t, hasGreeting,
					"comment allez-vous means 'how are you': got %q", output)
			},
		},

		// --- Phonetic/broken English from non-native speakers ---
		{
			name:     "Albanian phonetic hay lov",
			user:     "dionid2792",
			input:    "hay lov",
			notEqual: true,
			check: func(t *testing.T, input, output string) {
				lower := strings.ToLower(output)
				hasInterpretation := strings.Contains(lower, "hi") ||
					strings.Contains(lower, "hey") ||
					strings.Contains(lower, "love") ||
					strings.Contains(lower, "hello")
				assert.True(t, hasInterpretation,
					"phonetic 'hay lov' should be interpreted as 'hi love' or similar: got %q", output)
			},
		},
		{
			name:     "Albanian phonetic wecap beby",
			user:     "dionid2792",
			input:    "wecap beby",
			notEqual: true,
			check: func(t *testing.T, input, output string) {
				assert.NotEqual(t, strings.ToLower(input), strings.ToLower(output),
					"should interpret phonetic English, not pass through unchanged")
			},
		},
		{
			name:     "Phonetic nais is English slang unchanged",
			user:     "dionid2792",
			input:    "nais",
			wantSame: true,
		},

		// --- Edge cases: YouTube custom emoji preservation ---
		{
			name:     "YouTube custom emoji preserved",
			user:     "user",
			input:    ":hand-pink-waving: hello :face-blue-smiling:",
			wantSame: true,
		},
		{
			name:     "YouTube custom emoji only",
			user:     "user",
			input:    ":yt: :hand-pink-waving:",
			wantSame: true,
		},

		// --- Edge cases: URLs must not be modified ---
		{
			name:     "URL in message unchanged",
			user:     "BotRix",
			input:    "Check out https://www.youtube.com/watch?v=abc123 for more",
			wantSame: true,
		},

		// --- Edge cases: @mentions must not be modified ---
		{
			name:     "At-mention in message unchanged",
			user:     "Malaya",
			input:    "@Vickey you are so funny today",
			wantSame: true,
		},

		// --- Edge cases: very short messages ---
		{
			name:     "Single letter a unchanged",
			user:     "user",
			input:    "a",
			wantSame: true,
		},
		{
			name:     "OK unchanged",
			user:     "user",
			input:    "OK",
			wantSame: true,
		},
		{
			name:     "bye unchanged",
			user:     "user",
			input:    "bye",
			wantSame: true,
		},
		{
			name:     "gud slang unchanged",
			user:     "dionid2792",
			input:    "gud",
			wantSame: true,
		},

		// --- Edge cases: numbers and dates ---
		{
			name:     "Numbers and shorthand unchanged",
			user:     "user",
			input:    "2 ok",
			wantSame: true,
		},
		{
			name:     "Age number unchanged",
			user:     "user",
			input:    "am 45",
			wantSame: true,
		},

		// --- Edge cases: proper nouns / place names ---
		{
			name:     "From country name unchanged",
			user:     "dionid2792",
			input:    "From Albania",
			wantSame: true,
		},
		{
			name:     "Indian state name unchanged",
			user:     "manishshivhare",
			input:    "Gujarat",
			wantSame: true,
		},

		// --- Edge cases: garbled/unknown text ---
		{
			name:     "Unknown language kebela rragaca",
			user:     "dionid2792",
			input:    "kebela rragaca",
			notEqual: false,
			check: func(t *testing.T, input, output string) {
				// For unrecognizable text, either translate or pass through — but don't crash
				assert.NotEmpty(t, output, "should return something, not empty string")
			},
		},

		// --- Edge cases: mixed emoji with non-English ---
		{
			name:     "Turkish with multiple emoji preserved",
			user:     "Zafer",
			input:    "günaydın 🌞☕ nasılsın",
			notEqual: true,
			contains: []string{"🌞", "☕"},
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasMorning := strings.Contains(lower, "morning") ||
					strings.Contains(lower, "good morning")
				assert.True(t, hasMorning,
					"günaydın means good morning: got %q", output)
			},
		},

		// --- Edge cases: Arabic script ---
		{
			name:     "Arabic greeting must be translated",
			user:     "AhmedMgz",
			input:    "مرحبا كيف حالك",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasGreeting := strings.Contains(lower, "hello") ||
					strings.Contains(lower, "hi") ||
					strings.Contains(lower, "how are you")
				assert.True(t, hasGreeting,
					"Arabic greeting must be translated to English: got %q", output)
			},
		},

		// --- Edge cases: Korean ---
		{
			name:     "Korean greeting must be translated",
			user:     "kimchi",
			input:    "안녕하세요 반갑습니다",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasGreeting := strings.Contains(lower, "hello") ||
					strings.Contains(lower, "hi") ||
					strings.Contains(lower, "nice to meet")
				assert.True(t, hasGreeting,
					"Korean greeting must be translated: got %q", output)
			},
		},

		// --- Edge cases: Hebrew ---
		{
			name:     "Hebrew shalom must be translated",
			user:     "user",
			input:    "שלום מה שלומך",
			notEqual: true,
			check: func(t *testing.T, _, output string) {
				lower := strings.ToLower(output)
				hasGreeting := strings.Contains(lower, "hello") ||
					strings.Contains(lower, "peace") ||
					strings.Contains(lower, "how are you")
				assert.True(t, hasGreeting,
					"Hebrew greeting must be translated: got %q", output)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			chain := newTestChain(t)
			result, err := chain.Translate(ctx, tc.user, tc.input)
			require.NoError(t, err)
			t.Logf("input:  %q", tc.input)
			t.Logf("output: %q", result)

			if tc.wantSame {
				assert.Equal(t, tc.input, result, "should be returned unchanged")
				return
			}

			if tc.notEqual {
				assert.NotEqual(t, tc.input, result, "should be translated")
			}

			for _, s := range tc.contains {
				assert.Contains(t, strings.ToLower(result), strings.ToLower(s))
			}

			if tc.check != nil {
				tc.check(t, tc.input, result)
			}
		})
	}
}
