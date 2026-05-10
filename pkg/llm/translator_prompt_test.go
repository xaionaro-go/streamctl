package llm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectedTranslateEnglishWithHistory pins the exact byte sequence of
// BuildTranslatePrompt for TargetLang="English", user="alice",
// history="alice: hi\nbob: hola\n". Any silent edit to the rule list, the
// "/no_think" trailer, or the history-suffix shape will break this snapshot.
//
// The trailing newline after "bob: hola" is intentional — it is the newline
// that was present in the supplied history argument, copied verbatim into
// the prompt, and it must survive into the final string.
const expectedTranslateEnglishWithHistory = `Translate this chat message to English. The message may be mixed. Rules:
- Translate ALL non-English words to English
- Turkish endearments: aşkım/aşkim→my love, canım→my dear/my soul, güzel→beautiful
- Turkish: arkadaş/arkadaşım = friend/my friend (NOT 'dear' — 'dear' is canım)
- Turkish: havalı/havalısın = cool/stylish (NOT 'beautiful' or 'great' or 'good' — beautiful=güzel)
- Turkish imperatives: gönder/gonder = send (imperative, NOT past tense 'sent'). bana hediye gonder = send me a gift. Turkish bare verbs are commands/requests.
- Turkish: açıktım = 'I got hungry' (NOT 'turned on' or 'open')
- Turkish: 'o' is a pronoun meaning she/he/it/those — translate as 'those'/'they'/'it', NEVER as English 'oh'
  Example: 'o hep hazır yiyecekler' → 'those are always ready-made foods'
- Turkish: küsmek/küserim = to sulk, to give the cold shoulder, to stop talking out of offense. NEVER translate as 'fed up' or 'angry'. Example: 'sizden küserim' → 'I'll sulk at you' or 'I'll give you the cold shoulder'
- Turkish: 'misafir geleceğim/geleçeğim' = 'I will come as a guest' (the SPEAKER is visiting someone). The subject is 'I' (first person -im suffix). Do NOT translate as 'a guest is coming to me' — that reverses the meaning.
- Turkish: sıkılmak/sıkıldıysan = to be BORED (not 'tired'). Example: 'benden sıkıldıysan' → 'if you're bored of me'
- Russian slang: 'епта'/'ёпта' is a vulgar filler (like 'damn'), NOT an endearment
- Transliterated Russian/Ukrainian/Slavic: 'khavla hospodu' = 'praise the Lord' (хвала Господу). 'dobryi den' = 'good day' (добрый день, NOT evening). 'harasho'/'khorosho' = 'good/fine' (хорошо, NOT hello). 'kraciva'/'krasiva' = 'beautiful' (красива). 'perviy' = 'first' (первый). 'shcho tse' = 'what is this' (Ukrainian що це).
- French abbreviations: 'slt' = 'salut' (hello/hi)
- Indonesian: nyuci/mencuci=washing, masak=cooking, makan=eating, brpa/berapa=how much/what time, nambah cantik=getting more beautiful/prettier
- USERNAMES: If the sender's username contains a word that also appears in the message, that word is a name — keep it as-is, do NOT translate it. Example: user 'DewaJon' writes 'dewa juga lagi masak' — 'dewa' is their name, NOT the word for 'god'.
- Phonetic text (hay=hi, lov=love, beby=baby, wecap=WhatsApp): interpret and write correct English
- Phonetic/broken spelling from non-native speakers: decode each word phonetically. Examples: 'cen'='can', 'ai'='I', 'sey'='say', 'sllava'='slava/glory', 'mek'='make', 'naic'='nice', 'Famili'='family'. Translate the decoded meaning.
  Example: 'cen ai sey sllava Ukraina' → 'Can I say glory to Ukraine'
  Example: 'mek naic Famili' → 'Make nice family'
- Translate MEANING, NEVER transliterate. Hindi/Sanskrit नमस्कार/नमस्ते → 'Hello' (NEVER 'Namaskar' or 'Namaste'). Always use the English equivalent word.
- ABSOLUTELY NEVER add emoji that are not in the original message. Zero new emoji.
- Keep ALL original emoji exactly as-is
- Do NOT add content not implied by the original (no 'my love' unless source says it)
- Output ONLY the translated text, nothing else
The sender's username is: alice
/no_think

Recent chat for context:
alice: hi
bob: hola
`

// expectedTranslateEnglishEmptyHistory pins the prompt shape when history
// is empty. The "Recent chat for context" suffix MUST be absent — verifying
// that BuildTranslatePrompt does not emit the suffix line for empty input.
// Note: there is no trailing newline after "/no_think" — the function ends
// the string there and only appends the suffix block when history is non-empty.
const expectedTranslateEnglishEmptyHistory = `Translate this chat message to English. The message may be mixed. Rules:
- Translate ALL non-English words to English
- Turkish endearments: aşkım/aşkim→my love, canım→my dear/my soul, güzel→beautiful
- Turkish: arkadaş/arkadaşım = friend/my friend (NOT 'dear' — 'dear' is canım)
- Turkish: havalı/havalısın = cool/stylish (NOT 'beautiful' or 'great' or 'good' — beautiful=güzel)
- Turkish imperatives: gönder/gonder = send (imperative, NOT past tense 'sent'). bana hediye gonder = send me a gift. Turkish bare verbs are commands/requests.
- Turkish: açıktım = 'I got hungry' (NOT 'turned on' or 'open')
- Turkish: 'o' is a pronoun meaning she/he/it/those — translate as 'those'/'they'/'it', NEVER as English 'oh'
  Example: 'o hep hazır yiyecekler' → 'those are always ready-made foods'
- Turkish: küsmek/küserim = to sulk, to give the cold shoulder, to stop talking out of offense. NEVER translate as 'fed up' or 'angry'. Example: 'sizden küserim' → 'I'll sulk at you' or 'I'll give you the cold shoulder'
- Turkish: 'misafir geleceğim/geleçeğim' = 'I will come as a guest' (the SPEAKER is visiting someone). The subject is 'I' (first person -im suffix). Do NOT translate as 'a guest is coming to me' — that reverses the meaning.
- Turkish: sıkılmak/sıkıldıysan = to be BORED (not 'tired'). Example: 'benden sıkıldıysan' → 'if you're bored of me'
- Russian slang: 'епта'/'ёпта' is a vulgar filler (like 'damn'), NOT an endearment
- Transliterated Russian/Ukrainian/Slavic: 'khavla hospodu' = 'praise the Lord' (хвала Господу). 'dobryi den' = 'good day' (добрый день, NOT evening). 'harasho'/'khorosho' = 'good/fine' (хорошо, NOT hello). 'kraciva'/'krasiva' = 'beautiful' (красива). 'perviy' = 'first' (первый). 'shcho tse' = 'what is this' (Ukrainian що це).
- French abbreviations: 'slt' = 'salut' (hello/hi)
- Indonesian: nyuci/mencuci=washing, masak=cooking, makan=eating, brpa/berapa=how much/what time, nambah cantik=getting more beautiful/prettier
- USERNAMES: If the sender's username contains a word that also appears in the message, that word is a name — keep it as-is, do NOT translate it. Example: user 'DewaJon' writes 'dewa juga lagi masak' — 'dewa' is their name, NOT the word for 'god'.
- Phonetic text (hay=hi, lov=love, beby=baby, wecap=WhatsApp): interpret and write correct English
- Phonetic/broken spelling from non-native speakers: decode each word phonetically. Examples: 'cen'='can', 'ai'='I', 'sey'='say', 'sllava'='slava/glory', 'mek'='make', 'naic'='nice', 'Famili'='family'. Translate the decoded meaning.
  Example: 'cen ai sey sllava Ukraina' → 'Can I say glory to Ukraine'
  Example: 'mek naic Famili' → 'Make nice family'
- Translate MEANING, NEVER transliterate. Hindi/Sanskrit नमस्कार/नमस्ते → 'Hello' (NEVER 'Namaskar' or 'Namaste'). Always use the English equivalent word.
- ABSOLUTELY NEVER add emoji that are not in the original message. Zero new emoji.
- Keep ALL original emoji exactly as-is
- Do NOT add content not implied by the original (no 'my love' unless source says it)
- Output ONLY the translated text, nothing else
The sender's username is: alice
/no_think`

// expectedDetectEnglishEmptyHistory pins the language-detect prompt for
// TargetLang="English" and targetCode="en" with no history. This mirrors
// BuildLanguageDetectPrompt's full literal, including the long "Foreign proper
// nouns" sentence collapsed onto one line (the source uses a Go
// string-concatenation continuation, not a real \n). The "Recent chat:"
// suffix MUST be absent — verifying that the function does not emit the
// suffix line for empty input.
const expectedDetectEnglishEmptyHistory = `Classify this chat message. Reply with EXACTLY this format (no other text):
IS_TARGET: YES or NO
LANGUAGES: code1:confidence1, code2:confidence2, ...

IS_TARGET means: is this message written primarily in English?
Answer YES for English with typos, slang, abbreviations, or broken grammar.
Answer NO if an English-only speaker would NOT understand the overall meaning of the message.
Foreign proper nouns (country names, city names, brand names) do not count — if the sentence structure and grammar are English, answer YES even with a foreign proper noun in it.

Where confidence is 0.0 to 1.0. List up to 3 most likely ISO 639-1 language codes.

Target language: English (en)
/no_think`

// expectedDetectEnglishWithHistory pins the language-detect prompt when
// history is non-empty. The label is intentionally "Recent chat:" (not
// "Recent chat for context:" used by BuildTranslatePrompt) — that asymmetry
// is observable on the wire and must be preserved. The trailing newline
// after "bob: hola" is the newline copied verbatim from the supplied
// history string.
const expectedDetectEnglishWithHistory = `Classify this chat message. Reply with EXACTLY this format (no other text):
IS_TARGET: YES or NO
LANGUAGES: code1:confidence1, code2:confidence2, ...

IS_TARGET means: is this message written primarily in English?
Answer YES for English with typos, slang, abbreviations, or broken grammar.
Answer NO if an English-only speaker would NOT understand the overall meaning of the message.
Foreign proper nouns (country names, city names, brand names) do not count — if the sentence structure and grammar are English, answer YES even with a foreign proper noun in it.

Where confidence is 0.0 to 1.0. List up to 3 most likely ISO 639-1 language codes.

Target language: English (en)
/no_think

Recent chat:
alice: hi
bob: hola
`

func TestBuildTranslatePrompt_snapshot(t *testing.T) {
	tc := NewTranslatorChain("English", 20, nil)
	got := tc.BuildTranslatePrompt("alice", "alice: hi\nbob: hola\n")
	require.Equal(t, expectedTranslateEnglishWithHistory, got,
		"BuildTranslatePrompt snapshot drifted; if intentional, regenerate the snapshot, otherwise a rule was edited silently")
}

func TestBuildTranslatePrompt_emptyHistory(t *testing.T) {
	tc := NewTranslatorChain("English", 20, nil)
	got := tc.BuildTranslatePrompt("alice", "")
	require.Equal(t, expectedTranslateEnglishEmptyHistory, got)
	// Falsifier: an empty history must NOT produce the suffix line, and must
	// NOT produce a stray trailing newline after "/no_think".
	assert.NotContains(t, got, "Recent chat for context",
		"empty history must not emit the context suffix line")
	assert.False(t, strings.HasSuffix(got, "\n"),
		"empty history must not leave a dangling newline after /no_think")
	assert.True(t, strings.HasSuffix(got, "/no_think"),
		"prompt must end exactly at /no_think when history is empty")
}

func TestBuildLanguageDetectPrompt_snapshot(t *testing.T) {
	tc := NewTranslatorChain("English", 20, nil)
	got := tc.BuildLanguageDetectPrompt("en", "")
	require.Equal(t, expectedDetectEnglishEmptyHistory, got)
	// Falsifier: empty history must NOT emit the suffix line, and must NOT
	// leave a stray trailing newline after "/no_think".
	assert.NotContains(t, got, "Recent chat",
		"empty history must not emit the context suffix line")
	assert.False(t, strings.HasSuffix(got, "\n"),
		"empty history must not leave a dangling newline after /no_think")
	assert.True(t, strings.HasSuffix(got, "/no_think"),
		"prompt must end exactly at /no_think when history is empty")
}

func TestBuildLanguageDetectPrompt_withHistory(t *testing.T) {
	tc := NewTranslatorChain("English", 20, nil)
	got := tc.BuildLanguageDetectPrompt("en", "alice: hi\nbob: hola\n")
	require.Equal(t, expectedDetectEnglishWithHistory, got,
		"BuildLanguageDetectPrompt history-suffix snapshot drifted; if intentional, regenerate")
	// Falsifier: the deliberately asymmetric label must be present in this
	// path AND must NOT collapse to translate's "Recent chat for context:"
	// label.
	assert.Contains(t, got, "Recent chat:\nalice: hi\nbob: hola\n",
		"detect-prompt history block must use the 'Recent chat:' label")
	assert.NotContains(t, got, "Recent chat for context:",
		"detect-prompt MUST NOT use translate's 'Recent chat for context:' label")
}

func TestBuildTranslatePrompt_targetLanguage(t *testing.T) {
	tc := NewTranslatorChain("Russian", 20, nil)
	got := tc.BuildTranslatePrompt("alice", "")

	// Positive: the header line must name Russian as the target.
	assert.Contains(t, got, "Translate this chat message to Russian",
		"target-language header must follow TargetLang")
	// Positive: the "non-<target>" rule must use Russian.
	assert.Contains(t, got, "Translate ALL non-Russian words to Russian")
	// Positive: phonetic-text rule must close out with the target language.
	assert.Contains(t, got, "interpret and write correct Russian")

	// The rule list intentionally references English in operator-tuned content
	// (e.g. the "NEVER as English 'oh'" guidance and Hindi→English example).
	// Any "English" outside those known rule fragments would mean a placeholder
	// was missed during template substitution. Strip the known fragments and
	// then assert no remaining "English" survives.
	knownEnglishFragments := []string{
		"NEVER as English 'oh'",
		"Always use the English equivalent word.",
	}
	stripped := got
	for _, frag := range knownEnglishFragments {
		require.Contains(t, stripped, frag,
			"expected fragment %q must appear verbatim in the prompt", frag)
		stripped = strings.Replace(stripped, frag, "", 1)
	}
	assert.NotContains(t, stripped, "English",
		"after stripping known English-rule fragments, no stray 'English' must remain — "+
			"a leftover means BuildTranslatePrompt forgot a target-language placeholder")
}
