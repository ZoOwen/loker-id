package normalizer

import (
	"reflect"
	"testing"
)

func TestExtractStack(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		description string
		want        []string
	}{
		// --- positive cases ---
		{
			"golang mention in parens",
			"Backend Engineer (Golang)", "",
			[]string{"Go"},
		},
		{
			"capitalized standalone Go in title",
			"Kami cari developer Go untuk backend", "",
			[]string{"Go"},
		},
		{
			"golang plus other tech in description",
			"Backend Engineer", "Familiar with Golang, PostgreSQL, and Docker",
			[]string{"Docker", "Go", "PostgreSQL"},
		},
		{
			"frontend stack",
			"Frontend Developer", "React, TypeScript, dan Next.js",
			[]string{"Next.js", "React", "TypeScript"},
		},
		{
			"node backend stack",
			"Fullstack Developer", "Node.js dan Express untuk backend, MongoDB untuk database",
			[]string{"Express", "MongoDB", "Node.js"},
		},
		{
			"devops stack",
			"DevOps Engineer", "AWS, Kubernetes, Docker, Terraform",
			[]string{"AWS", "Docker", "Kubernetes", "Terraform"},
		},
		{
			"mobile stack",
			"Mobile Developer", "Flutter dan React Native",
			[]string{"Flutter", "React Native"},
		},
		{
			"dotnet stack, C# not confused with C++",
			".NET Developer", "ASP.NET Core, C#, SQL Server",
			[]string{".NET", "C#", "SQL Server"},
		},
		{
			"javascript does not also tag Java",
			"", "JavaScript developer wanted",
			[]string{"JavaScript"},
		},
		{
			"GCP mention does not also tag Go",
			"Software Engineer", "Familiar with Google Cloud Platform (GCP)",
			[]string{"GCP"},
		},
		{
			"all caps GO still counts as standalone",
			"BACKEND ENGINEER (GO)", "",
			[]string{"Go"},
		},

		// --- required negative cases: must NOT match ---
		{
			"lowercase go as an ordinary English verb — the canonical false-positive example",
			"", "we go the extra mile",
			nil,
		},
		{
			"lowercase go in another common phrase",
			"", "let's go above and beyond for our customers",
			nil,
		},
		{
			"R is never matched, even next to tech-sounding text",
			"R&D Engineer", "we do R&D on new HR systems",
			nil,
		},
		{
			"react must not match inside reactive",
			"", "we are looking for a reactive and dynamic person",
			nil,
		},
		{
			"go must not match inside going",
			"", "going forward, we will grow the team",
			nil,
		},
		{
			"go must not match inside google",
			"", "I love using google search every day",
			nil,
		},
		{
			"go must not match inside gopher",
			"", "gopher enthusiasts welcome",
			nil,
		},
		{
			"java must not match inside javascript-only mention beyond the JavaScript case above",
			"", "we use javascript, not enterprise software",
			[]string{"JavaScript"},
		},
		{
			"empty title and description",
			"", "",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractStack(tt.title, tt.description)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractStack(%q, %q) = %v, want %v", tt.title, tt.description, got, tt.want)
			}
		})
	}
}
