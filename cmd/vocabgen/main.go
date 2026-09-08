// Command vocabgen generates the packages under vocab/ from the published
// vocabulary documents listed in sources.go.
//
// Run it from the repository root:
//
//	go run ./cmd/vocabgen
//
// Each vocabulary becomes a directory holding a .ofn file — the ontology in
// OWL 2 functional syntax — and a Go file of typed constants. Downloads are
// cached, so a re-run costs nothing; -refresh fetches again.
//
// The program is deliberately noisy about what it could not translate. A
// vocabulary that suddenly reports many more skipped triples has either
// changed shape or found a gap in internal/rdfowl, and either way somebody
// should look before committing the result.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gowl/internal/rdf"
	"gowl/internal/rdfowl"
	"gowl/internal/vocabgen"
	"gowl/owl"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("vocabgen: ")

	out := flag.String("out", "vocab", "directory to write the generated packages into")
	cache := flag.String("cache", filepath.Join(os.TempDir(), "gowl-vocabgen"), "directory to cache downloads in")
	refresh := flag.Bool("refresh", false, "fetch the source documents again even when cached")
	only := flag.String("only", "", "comma-separated package names to generate (default: all)")
	verbose := flag.Bool("v", false, "list every triple that could not be translated")
	flag.Parse()

	wanted := map[string]bool{}
	for _, name := range strings.Split(*only, ",") {
		if name = strings.TrimSpace(name); name != "" {
			wanted[name] = true
		}
	}

	if err := os.MkdirAll(*cache, 0o755); err != nil {
		log.Fatal(err)
	}

	var generated []source
	for _, s := range sources {
		if len(wanted) > 0 && !wanted[s.pkg] {
			continue
		}
		if err := generate(s, *out, *cache, *refresh, *verbose); err != nil {
			log.Fatalf("%s: %v", s.pkg, err)
		}
		generated = append(generated, s)
	}

	if len(wanted) == 0 {
		if err := writeRegistry(*out, sources); err != nil {
			log.Fatalf("registry: %v", err)
		}
	} else if len(generated) != len(wanted) {
		log.Fatalf("-only named a vocabulary that is not in sources.go")
	}
}

func generate(s source, outDir, cacheDir string, refresh, verbose bool) error {
	doc, sum, err := fetch(s.url, cacheDir, refresh)
	if err != nil {
		return err
	}

	var g *rdf.Graph
	switch s.format {
	case "turtle":
		g, err = rdf.ParseTurtle(strings.NewReader(string(doc)), s.base)
	case "rdfxml":
		g, err = rdf.ParseXML(strings.NewReader(string(doc)), s.base)
	default:
		return fmt.Errorf("unknown format %q", s.format)
	}
	if err != nil {
		return err
	}

	res, err := rdfowl.Convert(g, rdfowl.Options{
		Namespaces:    s.allNamespaces(),
		IRI:           s.iri,
		RangeHints:    s.rangeHints,
		DatatypeRoots: s.datatypeRoots,
	})
	if err != nil {
		return err
	}
	res.Ontology.Prefixes = prefixes(res.Ontology.Prefixes, s)

	files, err := vocabgen.Generate(vocabgen.Package{
		Name:         s.pkg,
		Title:        s.title,
		Summary:      s.summary,
		SourceURL:    s.url,
		SourceSHA256: sum,
		Namespaces:   s.allNamespaces(),
		Prefix:       s.prefix,
		Namespace:    s.namespace,
		Ontology:     res.Ontology,
	})
	if err != nil {
		return err
	}

	dir := filepath.Join(outDir, s.pkg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, s.pkg+".ofn"), files.OFN, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, s.pkg+".go"), files.Go, 0o644); err != nil {
		return err
	}

	log.Printf("%-8s %4d terms  %5d triples  %5d axioms  %4d untranslated",
		s.pkg, files.Terms, len(g.Triples), res.Ontology.Len(), len(res.Skipped))
	if verbose {
		for _, skip := range res.Skipped {
			log.Printf("  %s", skip)
		}
	} else if reasons := summarise(res.Skipped); len(reasons) > 0 {
		log.Printf("         untranslated: %s", strings.Join(reasons, ", "))
	}
	return nil
}

// prefixes settles what the .ofn file abbreviates with. It keeps what the
// document declared, adds the vocabulary's customary prefix, and drops an
// empty prefix bound to the vocabulary's own namespace — several documents
// declare one, and it would otherwise win the tie and render prov:Activity
// as :Activity.
func prefixes(declared *owl.Prefixes, s source) *owl.Prefixes {
	out := owl.NewPrefixes()
	for _, name := range declared.Names() {
		ns, _ := declared.Namespace(name)
		if name == "" && string(ns) == s.namespace {
			continue
		}
		out.Set(name, ns)
	}
	out.Set(s.prefix, owl.IRI(s.namespace))
	return out
}

func summarise(skips []rdfowl.Skip) []string {
	counts := map[string]int{}
	for _, s := range skips {
		counts[s.Reason]++
	}
	out := make([]string, 0, len(counts))
	for reason, n := range counts {
		out = append(out, fmt.Sprintf("%s x%d", reason, n))
	}
	sort.Strings(out)
	return out
}

// fetch returns a document and its SHA-256, reading the cache when it can.
func fetch(url, cacheDir string, refresh bool) ([]byte, string, error) {
	key := sha256.Sum256([]byte(url))
	path := filepath.Join(cacheDir, hex.EncodeToString(key[:8])+".doc")

	if !refresh {
		if body, err := os.ReadFile(path); err == nil {
			return body, digest(body), nil
		}
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	// Several of these IRIs are the vocabulary's own namespace and serve
	// whichever syntax the client asks for.
	req.Header.Set("Accept", "text/turtle;q=1.0, application/rdf+xml;q=0.9, */*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return nil, "", err
	}
	return body, digest(body), nil
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
