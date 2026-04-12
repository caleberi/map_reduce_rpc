// Plugin: topwords
//
// Like the built-in word-count (wc) plugin but strips common English
// stopwords so that the output highlights meaningful vocabulary.
//
// Build:
//
//	go build -buildmode=plugin \
//	  -o plugins/topwords/topwords.so \
//	  ./plugins/topwords/topwords.go

package main

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/caleberi/map_reduce_rpc/mrp"
)

// stopwords is a set of high-frequency English words to ignore.
var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "as": true, "into": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"being": true, "have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "must": true, "shall": true, "can": true,
	"i": true, "me": true, "my": true, "we": true, "our": true, "you": true,
	"your": true, "he": true, "she": true, "it": true, "they": true, "them": true,
	"his": true, "her": true, "its": true, "their": true, "this": true, "that": true,
	"these": true, "those": true, "what": true, "which": true, "who": true,
	"not": true, "no": true, "so": true, "if": true, "then": true, "than": true,
	"up": true, "out": true, "about": true, "all": true, "also": true, "just": true,
	"more": true, "when": true, "there": true, "here": true, "how": true,
	"s": true, "t": true, "re": true, "ve": true, "ll": true, "d": true,
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
}

// Map emits (word, "1") for every non-stopword token of length ≥ 3.
func Map(_ string, contents string) []mrp.KeyValue {
	words := tokenize(contents)
	kvs := make([]mrp.KeyValue, 0, len(words))
	for _, w := range words {
		if len(w) < 3 || stopwords[w] {
			continue
		}
		kvs = append(kvs, mrp.KeyValue{Key: w, Value: "1"})
	}
	return kvs
}

// Reduce sums occurrence counts for each meaningful word.
func Reduce(_ string, values []string) string {
	total := 0
	for _, v := range values {
		n, err := strconv.Atoi(v)
		if err == nil {
			total += n
		}
	}
	return strconv.Itoa(total)
}
