// Package keygen derives and de-duplicates the human-readable project keys.
package keygen

import (
	"strconv"
	"strings"
)

// FallbackKey is used when a name has no usable ASCII word.
const FallbackKey = "SB"

// DeriveKeyPrefix mirrors the frontend's `deriveKeyPrefix`: uppercase the name,
// split on non-alphanumerics, keep words that contain an uppercase ASCII byte,
// then take the first three characters (one word) or the first character of up
// to four words.
func DeriveKeyPrefix(name string) string {
	words := make([]string, 0, 4)
	for _, word := range strings.FieldsFunc(strings.ToUpper(name), func(r rune) bool {
		return !isASCIIAlphanumeric(r)
	}) {
		if hasASCIIUpper(word) {
			words = append(words, word)
		}
	}

	if len(words) == 0 {
		return FallbackKey
	}

	if len(words) == 1 {
		word := words[0]
		prefix := word
		if len(prefix) > 3 {
			prefix = prefix[:3]
		}
		for len(prefix) < 2 {
			prefix += "X"
		}
		return prefix
	}

	if len(words) > 4 {
		words = words[:4]
	}

	var prefix strings.Builder
	for _, word := range words {
		prefix.WriteByte(word[0])
	}
	return prefix.String()
}

// UniqueKey appends the smallest free numeric suffix when base is taken.
func UniqueKey(base string, taken map[string]struct{}) string {
	if _, exists := taken[base]; !exists {
		return base
	}

	for index := 2; ; index++ {
		candidate := base + strconv.Itoa(index)
		if _, exists := taken[candidate]; !exists {
			return candidate
		}
	}
}

func isASCIIAlphanumeric(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func hasASCIIUpper(word string) bool {
	for index := range len(word) {
		if word[index] >= 'A' && word[index] <= 'Z' {
			return true
		}
	}
	return false
}
