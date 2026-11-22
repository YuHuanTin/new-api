package tokenkit

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tiktoken-go/tokenizer/codec"
)

func TestModelFamily(t *testing.T) {
	for _, tc := range []struct {
		model  string
		family Family
	}{
		{"claude-3-5-sonnet-20240620", FamilyClaude},
		{"claude-sonnet-4-20250514", FamilyClaude},
		{"claude-sonnet-4-5-20250929", FamilyClaude},
		{"claude-haiku-4-5", FamilyClaude},
		{"claude-sonnet-4-6", FamilyClaude},
		// Claude Opus 4.7 introduced the tokenizer every later Claude model uses.
		{"claude-opus-4-7", FamilyClaude47},
		{"claude-opus-4-7@20260416", FamilyClaude47},
		{"us.anthropic.claude-opus-4-7-v1:0", FamilyClaude47},
		{"claude-sonnet-5", FamilyClaude47},
		{"claude-fable-5-1", FamilyClaude47},
		{"Claude-Opus-5-5", FamilyClaude47},
		{"claude-mythos-preview", FamilyClaude47},
		{"gemini-3.7-flash", FamilyGemini},
		{"gpt-5.6-sol", FamilyOpenAI},
		{"gpt-6-astra", FamilyOpenAI},
		{"o3-mini", FamilyOpenAI},
		{"deepseek-v4", FamilyUnknown},
	} {
		assert.Equal(t, tc.family, ModelFamily(tc.model), tc.model)
	}
}

func TestEstimateUsesFamilyWeights(t *testing.T) {
	const text = "func main() {\n\tfmt.Println(\"统计 token\", 42)\n}\nSee https://example.com/a?b=1"
	require.NotEqual(t, EstimateFamily(FamilyClaude, text), EstimateFamily(FamilyClaude47, text))

	assert.Equal(t, EstimateFamily(FamilyClaude, text), Estimate("claude-haiku-4-5", text))
	assert.Equal(t, EstimateFamily(FamilyClaude47, text), Estimate("claude-sonnet-5", text))
	assert.Equal(t, EstimateFamily(FamilyUnknown, text), Estimate("deepseek-v4", text))
	assert.Equal(t, EstimateFamily(FamilyUnknown, text), EstimateFamily("other", text))
	assert.Zero(t, Estimate("claude-sonnet-5", ""))
}

func TestCountUsesTiktokenForOpenAIModels(t *testing.T) {
	const text = "大语言模型按 token 计费；Large language models bill by token. 🙂"
	o200k, _ := codec.NewO200kBase().Count(text)
	cl100k, _ := codec.NewCl100kBase().Count(text)
	require.NotEqual(t, o200k, cl100k)

	assert.Equal(t, o200k, Count("gpt-5.6-sol", text))
	assert.Equal(t, o200k, Count("gpt-6-astra", text))
	assert.Equal(t, o200k, Count("gpt-4o-mini", text))
	assert.Equal(t, cl100k, Count("gpt-4", text))
	assert.Equal(t, cl100k, Count("gpt-3.5-turbo", text))
	assert.Equal(t, Estimate("claude-sonnet-5", text), Count("claude-sonnet-5", text))
}

// Long runs without separators are cut before encoding so BPE stays linear;
// ordinary text is counted exactly and cut runs stay within 0.5%.
func TestCountCutsOnlyLongRuns(t *testing.T) {
	encoder := codec.NewO200kBase()
	ordinary := strings.Repeat("func main() {\n\tfmt.Println(\"统计 token，按模型计费。\", 42)\n}\n| a | b |\n|---|---|\n", 200)
	want, _ := encoder.Count(ordinary)
	assert.Equal(t, want, Count("gpt-5.6-sol", ordinary))

	rng := rand.New(rand.NewSource(1))
	var cjk strings.Builder
	for range 5000 {
		cjk.WriteRune(rune(0x4E00 + rng.Intn(0x9FA5-0x4E00)))
	}
	for _, text := range []string{cjk.String(), strings.Repeat("token", 2000), "a" + strings.Repeat(" ", 10000) + "b", strings.Repeat("=", 10000)} {
		exact, _ := encoder.Count(text)
		assert.InEpsilon(t, exact, Count("gpt-5.6-sol", text), 0.005)
	}
}

func TestZeroAllocBPEParityWithTiktoken(t *testing.T) {
	o200kOracle := codec.NewO200kBase()
	cl100kOracle := codec.NewCl100kBase()

	cases := []string{
		"",
		" ",
		"   ",
		"\n",
		"\r\n\r\n",
		"   \n  \n  hello",
		"  hello   world  ",
		" 123 4567 89012345",
		"I'm don't you're we've they'll she'd HE'S THEY'RE WE'LL",
		"a'Re b'rE c'Ve d'Ll e'S f'T g'M h'D 's 't 're 've 'm 'll 'd",
		" 'hello 'world ''test",
		"HelloWorld HTTPServer getURLPath camelCase ALLCAPS A",
		"你好World hello你好 你A! 你A's 测试12345 こんにちは 안녕하세요",
		"e\u0301 \u0301 \u0301abc a\u0301B ǅa ʰa \u0301's A\u0301's",
		" !@#\n/foo/bar//\r\n/baz ...??? === ---",
		" /\r\n/ \r\n/ /\n\r/",
		"🚀👨‍👩‍👧‍👦🙂\u00a0hello\t\tworld\v\ftest\u2000\u3000end",
		"<|endoftext|>hello<|fim_prefix|>world<|endofprompt|>",
		// 覆盖单段 > 128 字节触发 bpeMergeCount 缓冲区扩容路径
		strings.Repeat("abcdefghij", 20),
		strings.Repeat("一二三四五六七八九十", 15),
		// 覆盖各类 Unicode 类别（数字/标点/符号/控制符，排除 \x7f 因 tiktoken-go/regexp2 存在吞掉 0x7f 的上游 bug）
		"١٢٣٤ ⅣⅤⅥ ½¾ ∑∫∂√∞ ≠≤≥ §¶†‡ «»“”‘’ \x00\x01\x07\x1f",
		`{"model":"gpt-4o","messages":[{"role":"user","content":"func check(id int, q float64) (int, error) { return id, nil }"}]}`,
		strings.Repeat("Turn 1: check mem allocs in handler_1.go and verify slice cap growth. 统计 token 计费。\n", 50),
	}

	for _, text := range cases {
		wantO200k, err := o200kOracle.Count(text)
		require.NoError(t, err)
		assert.Equal(t, wantO200k, Count("gpt-4o", text), "o200k mismatch for %q", text)

		wantCl100k, err := cl100kOracle.Count(text)
		require.NoError(t, err)
		assert.Equal(t, wantCl100k, Count("gpt-4", text), "cl100k mismatch for %q", text)
	}

	// 官方 tiktoken 将 0x7f 编为单字节 token（1），且非法 UTF-8 字节不会 panic 或死循环
	assert.Equal(t, 1, Count("gpt-4o", "\x7f"))
	assert.Equal(t, 1, Count("gpt-4", "\x7f"))
	invalidUTF8 := string([]byte{0xff, 0xfe, 'a', 'b', 0xc3, 0x28, ' ', 0xe2, 0x82, 0x28})
	assert.Greater(t, Count("gpt-4o", invalidUTF8), 0)
	assert.Greater(t, Count("gpt-4", invalidUTF8), 0)
}

// FuzzZeroAllocBPEParityWithTiktoken 通过模糊测试验证手写预分词器在任意合法 UTF-8 输入下与官方 tiktoken-go 逐 token 一致。
func FuzzZeroAllocBPEParityWithTiktoken(f *testing.F) {
	o200kOracle := codec.NewO200kBase()
	cl100kOracle := codec.NewCl100kBase()

	for _, seed := range []string{
		"hello world",
		"I'm don't WE'RE",
		"你好World! 12345\r\n/path",
		"e\u0301\u0301B 's  \n  ",
		"  ?!\r\n/a\u00a0b",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 512 {
			text = text[:512]
		}
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0x7f) {
			return
		}
		// 仅跳过 tiktoken-go v0.8.1 已知的“换行+纯空白+换行”空行拆分 bug（保留普通多行含空格文本测试）
		if hasIndentedBlankLine(text) {
			return
		}
		wantO200k, err := o200kOracle.Count(text)
		require.NoError(t, err)
		gotO200k := o200kBase().count(text)
		if gotO200k != wantO200k {
			t.Fatalf("o200k mismatch for %q (bytes %x): got=%d want=%d", text, []byte(text), gotO200k, wantO200k)
		}

		wantCl100k, err := cl100kOracle.Count(text)
		require.NoError(t, err)
		gotCl100k := cl100kBase().count(text)
		if gotCl100k != wantCl100k {
			t.Fatalf("cl100k mismatch for %q (bytes %x): got=%d want=%d", text, []byte(text), gotCl100k, wantCl100k)
		}
	})
}

func hasIndentedBlankLine(s string) bool {
	seenNL := false
	seenSpaceAfterNL := false
	for _, r := range s {
		if r == '\r' || r == '\n' {
			if seenNL && seenSpaceAfterNL {
				return true
			}
			seenNL = true
			seenSpaceAfterNL = false
		} else if seenNL && (r == ' ' || r == '\t' || r == '\v' || r == '\f' || r == '\u00a0') {
			seenSpaceAfterNL = true
		} else {
			seenNL = false
			seenSpaceAfterNL = false
		}
	}
	return false
}

func BenchmarkCountLargePayload(b *testing.B) {
	snippet := "func check(id int, q float64, buf []byte, ok bool) (int, error) { if q < 0 || !ok { return 0, errBad } n := copy(buf, src); return n + id, nil } // check quota clamp & buffer reuse\n"
	text := strings.Repeat(snippet, 6500) // ~1.2 MB (~340k-400k tokens)

	// 预热词表，避免将一次性初始化计入热路径
	_ = Count("gpt-4o", "warmup")
	oracle := codec.NewO200kBase()
	_, _ = oracle.Count("warmup")

	b.Run("ZeroAlloc_o200k", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_ = Count("gpt-4o", text)
		}
	})
	b.Run("TiktokenRegexp2_o200k", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = oracle.Count(text)
		}
	})
}
