package normalizer

import (
	"regexp"
	"sort"
	"strings"
)

// matcher reports whether text contains this technology's marker.
type matcher func(text string) bool

// stackKeyword is one curated technology: it's tagged if any of its
// matchers fire anywhere in the combined title+description text.
type stackKeyword struct {
	name     string
	matchers []matcher
}

func (k stackKeyword) matches(text string) bool {
	for _, m := range k.matchers {
		if m(text) {
			return true
		}
	}
	return false
}

// word builds a case-insensitive matcher for alias. A plain alphanumeric
// alias ("golang", "kubernetes") is matched as a standalone word so it
// can't fire on a substring of some unrelated word ("java" inside
// "javascript"). An alias containing symbols or spaces ("c++", "c#",
// "react native") is matched as a plain substring instead: \b behaves
// oddly around non-word characters, but the symbols already make these
// tokens unambiguous enough that a bare substring match is safe.
func word(alias string) matcher {
	var re *regexp.Regexp
	if isPlainAlnum(alias) {
		re = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(alias) + `\b`)
	} else {
		re = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(alias))
	}
	return re.MatchString
}

func isPlainAlnum(s string) bool {
	for _, r := range s {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alnum {
			return false
		}
	}
	return true
}

// capitalized builds a case-SENSITIVE matcher for a tech name that
// collides with an ordinary English word — go, react, rust, spring,
// express, swift are all things people write in plain lowercase prose
// ("we go the extra mile", "a reactive team", "rust never sleeps"). It
// only matches the word written as Title-case or ALL-CAPS ("Go"/"GO",
// "React"/"REACT", ...), which is how these actually get written when
// they mean the technology. This deliberately misses an all-lowercase
// mention of the real technology in exchange for never false-positiving
// on the common word — the trade the task explicitly asked for.
func capitalized(word string) matcher {
	return capitalizedRe(word).MatchString
}

func capitalizedRe(word string) *regexp.Regexp {
	title := strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
	allCaps := strings.ToUpper(word)
	return regexp.MustCompile(`\b(` + regexp.QuoteMeta(title) + `|` + regexp.QuoteMeta(allCaps) + `)\b`)
}

// capitalizedExcept builds on capitalized(word), but ignores a match when
// the text immediately following it satisfies exclude — for "React",
// which must not also fire wherever "React Native" (a different,
// mobile-only technology) is mentioned.
func capitalizedExcept(word string, exclude *regexp.Regexp) matcher {
	re := capitalizedRe(word)
	return func(text string) bool {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			if exclude.MatchString(text[loc[1]:]) {
				continue
			}
			return true
		}
		return false
	}
}

// wordExceptPrecededBy builds on word(alias), but ignores a match
// immediately preceded by precedingChar — "JavaScript"'s "js" alias must
// not also fire on the ".js" suffix of some other framework's own name
// ("Node.js", "Vue.js", "Next.js", "Express.js" all contain a
// standalone-looking "js").
func wordExceptPrecededBy(alias string, precedingChar byte) matcher {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(alias) + `\b`)
	return func(text string) bool {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			if loc[0] > 0 && text[loc[0]-1] == precedingChar {
				continue
			}
			return true
		}
		return false
	}
}

var reactNativeSuffixRe = regexp.MustCompile(`(?i)^[\s-]*native\b`)

// stackKeywords is the curated tech list. Deliberately excluded: "R" (per
// spec, never matched — a bare single letter is far too ambiguous) and
// bare "C" or "node" for the same reason (unqualified "C" and "node" are
// ordinary words/abbreviations in non-tech contexts too; "c++"/"c#" and
// "node.js"/"nodejs" are unambiguous enough to keep).
var stackKeywords = []stackKeyword{
	{"Go", []matcher{capitalized("go"), word("golang")}},
	{"JavaScript", []matcher{word("javascript"), wordExceptPrecededBy("js", '.')}},
	{"TypeScript", []matcher{word("typescript")}},
	{"Python", []matcher{word("python")}},
	{"Java", []matcher{word("java")}},
	{"PHP", []matcher{word("php")}},
	{"Ruby", []matcher{word("ruby")}},
	{"Kotlin", []matcher{word("kotlin")}},
	{"Swift", []matcher{capitalized("swift")}},
	{"Rust", []matcher{capitalized("rust")}},
	{"Dart", []matcher{word("dart")}},
	{"C++", []matcher{word("c++")}},
	{"C#", []matcher{word("c#")}},

	{"React", []matcher{capitalizedExcept("react", reactNativeSuffixRe), word("react.js"), word("reactjs")}},
	{"React Native", []matcher{word("react native"), word("react-native")}},
	{"Vue", []matcher{word("vue"), word("vue.js"), word("vuejs")}},
	{"Angular", []matcher{word("angular")}},
	{"Svelte", []matcher{word("svelte")}},
	{"Next.js", []matcher{word("next.js"), word("nextjs")}},

	{"Node.js", []matcher{word("node.js"), word("nodejs")}},
	{"Express", []matcher{capitalized("express"), word("express.js"), word("expressjs")}},
	{"Django", []matcher{word("django")}},
	{"Flask", []matcher{word("flask")}},
	{"Laravel", []matcher{word("laravel")}},
	{"Spring", []matcher{capitalized("spring"), word("spring boot"), word("springboot")}},
	{"Rails", []matcher{word("rails"), word("ruby on rails")}},
	{"FastAPI", []matcher{word("fastapi"), word("fast api")}},
	{".NET", []matcher{word(".net"), word("dotnet"), word("asp.net")}},

	{"Flutter", []matcher{word("flutter")}},
	{"Android", []matcher{word("android")}},
	{"iOS", []matcher{word("ios")}},

	{"PostgreSQL", []matcher{word("postgresql"), word("postgres")}},
	{"MySQL", []matcher{word("mysql")}},
	{"MongoDB", []matcher{word("mongodb"), word("mongo")}},
	{"Redis", []matcher{word("redis")}},
	{"SQLite", []matcher{word("sqlite")}},
	{"SQL Server", []matcher{word("sql server"), word("mssql")}},

	{"Docker", []matcher{word("docker")}},
	{"Kubernetes", []matcher{word("kubernetes"), word("k8s")}},
	{"AWS", []matcher{word("aws")}},
	{"GCP", []matcher{word("gcp"), word("google cloud")}},
	{"Azure", []matcher{word("azure")}},
	{"Terraform", []matcher{word("terraform")}},

	{"GraphQL", []matcher{word("graphql")}},
	{"RabbitMQ", []matcher{word("rabbitmq")}},
	{"Kafka", []matcher{word("kafka")}},
	{"Git", []matcher{word("git")}},
}

// ExtractStack matches title and description against the curated tech
// list and returns the technologies found, alphabetically sorted.
func ExtractStack(title, description string) []string {
	text := title + "\n" + description

	var stack []string
	for _, k := range stackKeywords {
		if k.matches(text) {
			stack = append(stack, k.name)
		}
	}

	sort.Strings(stack)
	return stack
}
