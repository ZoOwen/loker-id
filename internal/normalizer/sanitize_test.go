package normalizer

import "testing"

func TestSanitizeDescription(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty input", "", ""},
		{"plain text passes through", "Just plain text", "Just plain text"},
		{
			"simple paragraph",
			"<p>Hello world.</p>",
			"Hello world.",
		},
		{
			"paragraph with class attribute stripped",
			`<p class="text-justify">Justified text</p>`,
			"Justified text",
		},
		{
			"unordered list becomes dash bullets on consecutive lines",
			"<ul><li>Item one</li><li>Item two</li></ul>",
			"- Item one\n- Item two",
		},
		{
			"two paragraphs separated by one blank line",
			"<p>First.</p><p>Second.</p>",
			"First.\n\nSecond.",
		},
		{
			"br becomes a line break",
			"Line one<br>Line two",
			"Line one\nLine two",
		},
		{
			"self-closing br",
			"Line one<br/>Line two",
			"Line one\nLine two",
		},
		{
			"inline formatting stripped, text kept together",
			"<li>Familiar with <strong>Docker</strong> and <em>Kubernetes</em></li>",
			"- Familiar with Docker and Kubernetes",
		},
		{
			"a word split across an inline tag boundary is rejoined — this is the mechanism bug 1 fixes for bug 4's ExtractStack failures",
			"<li>Doc<b>ker</b> and Kubernetes</li>",
			"- Docker and Kubernetes",
		},
		{
			"html entities are decoded",
			"<p>Tech &amp; Design</p>",
			"Tech & Design",
		},
		{
			"internal whitespace and indentation from source markup collapse",
			"<li>\n\t\tSome   item\n\t</li>",
			"- Some item",
		},
		{
			"script tag content is dropped entirely, never treated as visible text",
			"<p>Real content</p><script>alert('x'); var stack=['a','b'];</script>",
			"Real content",
		},
		{
			"style tag content is dropped entirely",
			"<style>.foo{color:red}</style><p>Real content</p>",
			"Real content",
		},
		{
			"realistic combined description shape",
			"<ul>\n\t<li>Building data engineering capability</li>\n\t<li>Lead team to assess Big Data Ecosystem</li>\n</ul>\n\n<ul>\n\t<li>Bachelor degree in Informatics Engineering</li>\n\t<li>Strong in Python, Java, Kafka</li>\n</ul>",
			"- Building data engineering capability\n- Lead team to assess Big Data Ecosystem\n\n- Bachelor degree in Informatics Engineering\n- Strong in Python, Java, Kafka",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeDescription(tt.in); got != tt.want {
				t.Errorf("SanitizeDescription(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSanitizeDescription_OutputIsMeaningfullySmaller demonstrates the
// actual complaint in bug 1 ("Response API sekarang kegedean"): cleaned
// output should be substantially smaller than markup-heavy raw HTML for
// realistic content.
func TestSanitizeDescription_OutputIsMeaningfullySmaller(t *testing.T) {
	raw := `<ul>
	<li>Bachelor degree in Informatics Engineering</li>
	<li>Strong technical leadership and management</li>
	<li>Strong in programming language such as Python, Java, Scala (software engineering) and experienced using Kafka, RabbitMQ, NoSQL, Spark, Cloud (Analytics Tech Stack)</li>
	<li>Good Critical thinking, problem-solving and conflict resolution</li>
	<li>Experience on building scalable data solution</li>
	<li>Preferably has 3 to 5 years of technical management experience</li>
	<li>Has 6 to 10 years of solution development and technology experience (Senior Data/Software Engineer) will be an advantage</li>
</ul>`

	got := SanitizeDescription(raw)
	if len(got) >= len(raw) {
		t.Errorf("SanitizeDescription output (%d bytes) is not smaller than raw input (%d bytes)", len(got), len(raw))
	}
	if got == "" {
		t.Error("SanitizeDescription stripped everything, want the actual content preserved")
	}
}
