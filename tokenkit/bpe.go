package tokenkit

import (
	"math"
	"unicode"
	"unicode/utf8"

	"github.com/tiktoken-go/tokenizer"
)

// bpeCounter 使用手写零分配预分词器 + 复用 BPE 缓冲区实现精确 tiktoken 计数，
// 彻底绕开 tiktoken-go 底层 regexp2 的 []rune 转换与海量 Match 堆分配。
type bpeCounter struct {
	vocab map[string]uint
	o200k bool
}

type bpePart struct {
	offset int
	rank   uint
}

// newBPECounter 从 tiktoken-go Codec 提取一次词表后即释放 Codec（连同其 regexp2 与反向词表）。
func newBPECounter(c tokenizer.Codec, maxID uint, o200k bool) *bpeCounter {
	v := make(map[string]uint, maxID)
	var idBuf [1]uint
	for id := range maxID {
		idBuf[0] = id
		if tok, err := c.Decode(idBuf[:]); err == nil {
			v[tok] = id
		}
	}
	return &bpeCounter{
		vocab: v,
		o200k: o200k,
	}
}

// count 精确统计 text 的 token 数量，全过程零堆分配（常规词元长度 <= 128 字节）。
func (b *bpeCounter) count(text string) int {
	if text == "" {
		return 0
	}
	var stackBuf [128]bpePart
	partsBuf := stackBuf[:0]
	total := 0

	for len(text) > 0 {
		var end int
		if b.o200k {
			end = nextPieceO200k(text)
		} else {
			end = nextPieceCl100k(text)
		}
		if end <= 0 {
			_, size := utf8.DecodeRuneInString(text)
			end = max(size, 1)
		}
		piece := text[:end]
		text = text[end:]

		if len(piece) == 1 {
			total++
			continue
		}
		if _, ok := b.vocab[piece]; ok {
			total++
			continue
		}
		total += b.bpeMergeCount(piece, &partsBuf)
	}
	return total
}

// bpeMergeCount 对未直接命中词表的单个词元执行字节对合并（BPE），复用 partsBuf 避免堆分配。
func (b *bpeCounter) bpeMergeCount(piece string, partsBuf *[]bpePart) int {
	n := len(piece) + 1
	if cap(*partsBuf) < n {
		*partsBuf = make([]bpePart, n)
	}
	parts := (*partsBuf)[:n]
	for i := range n {
		parts[i] = bpePart{offset: i, rank: math.MaxUint}
	}

	getRank := func(index, skip int) uint {
		if index+skip+2 < len(parts) {
			start := parts[index].offset
			end := parts[index+skip+2].offset
			if rank, ok := b.vocab[piece[start:end]]; ok {
				return rank
			}
		}
		return math.MaxUint
	}

	for i := range len(parts) - 2 {
		parts[i].rank = getRank(i, 0)
	}

	for len(parts) > 1 {
		minRank := uint(math.MaxUint)
		minIndex := 0
		for i := range len(parts) - 1 {
			if parts[i].rank < minRank {
				minRank = parts[i].rank
				minIndex = i
			}
		}
		if minRank == math.MaxUint {
			break
		}

		parts[minIndex].rank = getRank(minIndex, 1)
		if minIndex > 0 {
			parts[minIndex-1].rank = getRank(minIndex-1, 1)
		}
		copy(parts[minIndex+1:], parts[minIndex+2:])
		parts = parts[:len(parts)-1]
	}

	return len(parts) - 1
}

// 字符类别（与 o200k_base / cl100k_base 正则中的 Unicode 分类严格对齐）
type charCat uint8

const (
	catOther   charCat = iota
	catU               // Lu, Lt（仅大写/标题字母）
	catL               // Ll（仅小写字母）
	catUL              // Lm, Lo（修饰/其他字母，如中文，同时属于 U 与 L）
	catMark            // M（组合音标，同时属于 U、L 以及 [^\r\n\p{L}\p{N}]）
	catNum             // N（数字）
	catSpace           // 除 \r \n 外的空白符
	catNewline         // \r 或 \n
)

func classifyRune(s string) (r rune, size int, cat charCat) {
	b := s[0]
	if b < utf8.RuneSelf {
		switch {
		case 'a' <= b && b <= 'z':
			return rune(b), 1, catL
		case 'A' <= b && b <= 'Z':
			return rune(b), 1, catU
		case '0' <= b && b <= '9':
			return rune(b), 1, catNum
		case b == '\r' || b == '\n':
			return rune(b), 1, catNewline
		case b == ' ' || b == '\t' || b == '\v' || b == '\f':
			return rune(b), 1, catSpace
		default:
			return rune(b), 1, catOther
		}
	}
	r, size = utf8.DecodeRuneInString(s)
	switch {
	case unicode.Is(unicode.Ll, r):
		return r, size, catL
	case unicode.Is(unicode.Lu, r) || unicode.Is(unicode.Lt, r):
		return r, size, catU
	case unicode.Is(unicode.Lm, r) || unicode.Is(unicode.Lo, r):
		return r, size, catUL
	case unicode.IsMark(r):
		return r, size, catMark
	case unicode.IsNumber(r):
		return r, size, catNum
	case unicode.IsSpace(r):
		return r, size, catSpace
	default:
		return r, size, catOther
	}
}

func isO200kU(cat charCat) bool {
	return cat == catU || cat == catUL || cat == catMark
}

func isO200kL(cat charCat) bool {
	return cat == catL || cat == catUL || cat == catMark
}

func isLetterCat(cat charCat) bool {
	return cat == catU || cat == catL || cat == catUL
}

func isWordPrefixCat(cat charCat) bool {
	return cat != catNewline && !isLetterCat(cat) && cat != catNum
}

func isSpaceCat(cat charCat) bool {
	return cat == catSpace || cat == catNewline
}

// matchContraction 匹配 (?i:'s|'t|'re|'ve|'m|'ll|'d)，返回匹配字节数（0 表示不匹配）。
func matchContraction(s string) int {
	if len(s) < 2 || s[0] != '\'' {
		return 0
	}
	c1 := s[1] | 0x20 // ASCII 小写折叠
	switch c1 {
	case 's', 't', 'm', 'd':
		return 2
	case 'r':
		if len(s) >= 3 && (s[2]|0x20) == 'e' {
			return 3
		}
	case 'v':
		if len(s) >= 3 && (s[2]|0x20) == 'e' {
			return 3
		}
	case 'l':
		if len(s) >= 3 && (s[2]|0x20) == 'l' {
			return 3
		}
	}
	return 0
}

// matchO200kAlt1From 从 start 尝试匹配 [\p{Lu}\p{Lt}\p{Lm}\p{Lo}\p{M}]*[\p{Ll}\p{Lm}\p{Lo}\p{M}]+。
func matchO200kAlt1From(s string, start int) int {
	pos := start
	lastBothEnd := -1
	for pos < len(s) {
		_, sz, cat := classifyRune(s[pos:])
		if !isO200kU(cat) {
			break
		}
		pos += sz
		if cat == catUL || cat == catMark {
			lastBothEnd = pos
		}
	}
	uEnd := pos
	for pos < len(s) {
		_, sz, cat := classifyRune(s[pos:])
		if !isO200kL(cat) {
			break
		}
		pos += sz
	}
	if pos > uEnd {
		return pos
	}
	// U* 贪心吃掉了同时属于 U 和 L 的字符（如中文/音标），回退到最后一个同时属于 L 的字符末尾即可满足 L+
	if lastBothEnd > start {
		return lastBothEnd
	}
	return 0
}

// matchO200kAlt2From 从 start 尝试匹配 [\p{Lu}\p{Lt}\p{Lm}\p{Lo}\p{M}]+[\p{Ll}\p{Lm}\p{Lo}\p{M}]*。
func matchO200kAlt2From(s string, start int) int {
	pos := start
	for pos < len(s) {
		_, sz, cat := classifyRune(s[pos:])
		if !isO200kU(cat) {
			break
		}
		pos += sz
	}
	if pos == start {
		return 0
	}
	for pos < len(s) {
		_, sz, cat := classifyRune(s[pos:])
		if !isO200kL(cat) {
			break
		}
		pos += sz
	}
	return pos
}

// nextPieceO200k 实现 o200k_base 的零分配预分词状态机，与官方正则逐分支等价。
func nextPieceO200k(s string) int {
	_, sz0, cat0 := classifyRune(s)
	hasPrefix := isWordPrefixCat(cat0)

	// 分支 1：[^\r\n\p{L}\p{N}]?[\p{Lu}\p{Lt}\p{Lm}\p{Lo}\p{M}]*[\p{Ll}\p{Lm}\p{Lo}\p{M}]+(?i:'s|'t|'re|'ve|'m|'ll|'d)?
	if hasPrefix && sz0 < len(s) {
		if end := matchO200kAlt1From(s, sz0); end > 0 {
			return end + matchContraction(s[end:])
		}
	}
	if end := matchO200kAlt1From(s, 0); end > 0 {
		return end + matchContraction(s[end:])
	}

	// 分支 2：[^\r\n\p{L}\p{N}]?[\p{Lu}\p{Lt}\p{Lm}\p{Lo}\p{M}]+[\p{Ll}\p{Lm}\p{Lo}\p{M}]*(?i:'s|'t|'re|'ve|'m|'ll|'d)?
	if hasPrefix && sz0 < len(s) {
		if end := matchO200kAlt2From(s, sz0); end > 0 {
			return end + matchContraction(s[end:])
		}
	}
	if end := matchO200kAlt2From(s, 0); end > 0 {
		return end + matchContraction(s[end:])
	}

	// 分支 3：\p{N}{1,3}
	if cat0 == catNum {
		pos := sz0
		for count := 1; count < 3 && pos < len(s); count++ {
			_, sz, cat := classifyRune(s[pos:])
			if cat != catNum {
				break
			}
			pos += sz
		}
		return pos
	}

	// 分支 4： ?[^\s\p{L}\p{N}]+[\r\n/]*
	if end := matchPunctRun(s, true); end > 0 {
		return end
	}

	// 分支 5/6/7：\s*[\r\n]+ | \s+(?!\S) | \s+
	return matchWhitespaceRun(s)
}

// nextPieceCl100k 实现 cl100k_base 的零分配预分词状态机，与官方正则逐分支等价。
func nextPieceCl100k(s string) int {
	// 分支 1：(?i:'s|'t|'re|'ve|'m|'ll|'d)
	if n := matchContraction(s); n > 0 {
		return n
	}

	_, sz0, cat0 := classifyRune(s)

	// 分支 2：[^\r\n\p{L}\p{N}]?\p{L}+
	if isLetterCat(cat0) {
		pos := sz0
		for pos < len(s) {
			_, sz, cat := classifyRune(s[pos:])
			if !isLetterCat(cat) {
				break
			}
			pos += sz
		}
		return pos
	}
	if isWordPrefixCat(cat0) && sz0 < len(s) {
		_, sz1, cat1 := classifyRune(s[sz0:])
		if isLetterCat(cat1) {
			pos := sz0 + sz1
			for pos < len(s) {
				_, sz, cat := classifyRune(s[pos:])
				if !isLetterCat(cat) {
					break
				}
				pos += sz
			}
			return pos
		}
	}

	// 分支 3：\p{N}{1,3}
	if cat0 == catNum {
		pos := sz0
		for count := 1; count < 3 && pos < len(s); count++ {
			_, sz, cat := classifyRune(s[pos:])
			if cat != catNum {
				break
			}
			pos += sz
		}
		return pos
	}

	// 分支 4： ?[^\s\p{L}\p{N}]+[\r\n]*
	if end := matchPunctRun(s, false); end > 0 {
		return end
	}

	// 分支 5/6/7：\s*[\r\n]+ | \s+(?!\S) | \s+
	return matchWhitespaceRun(s)
}

// matchPunctRun 匹配 ` ?[^\s\p{L}\p{N}]+[\r\n/]*`（o200k 允许尾部 `/`）或 ` ?[^\s\p{L}\p{N}]+[\r\n]*`（cl100k）。
func matchPunctRun(s string, allowTrailingSlash bool) int {
	pos := 0
	if s[0] == ' ' {
		pos = 1
	}
	start := pos
	for pos < len(s) {
		_, sz, cat := classifyRune(s[pos:])
		if isSpaceCat(cat) || isLetterCat(cat) || cat == catNum {
			break
		}
		pos += sz
	}
	if pos == start {
		return 0
	}
	for pos < len(s) {
		b := s[pos]
		if b == '\r' || b == '\n' || (allowTrailingSlash && b == '/') {
			pos++
			continue
		}
		break
	}
	return pos
}

// matchWhitespaceRun 匹配 \s*[\r\n]+ | \s+(?!\S) | \s+。
func matchWhitespaceRun(s string) int {
	pos := 0
	lastNewlineEnd := 0
	prevPos := 0
	count := 0
	for pos < len(s) {
		_, sz, cat := classifyRune(s[pos:])
		if !isSpaceCat(cat) {
			break
		}
		prevPos = pos
		pos += sz
		count++
		if cat == catNewline {
			lastNewlineEnd = pos
		}
	}
	if lastNewlineEnd > 0 {
		return lastNewlineEnd
	}
	if count == 0 {
		return 0
	}
	// 后接非空白且空白数量 >= 2：\s+(?!\S) 保留最后一个空白符给下一个词元做前缀
	if pos < len(s) && count >= 2 {
		return prevPos
	}
	return pos
}
