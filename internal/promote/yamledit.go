package promote

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// SnapshotValues is the per-site snapshot config that lands in the
// matrix entry. Maps 1:1 to the chart's siteInstall.snapshot.* values.
type SnapshotValues struct {
	Ref    string
	S3Key  string
	Bucket string
}

// EditApplicationSet rewrites the matrix element matching siteKey to
// contain the given snapshot values. The function preserves comments,
// ordering, and indentation by editing yaml.Node trees in place.
//
// Layout expected (matches frankenpress/gitops-fp/apps/applicationset.yaml):
//
//	spec:
//	  generators:
//	    - matrix:
//	        generators:
//	          - list:
//	              elements:
//	                - site: sts
//	                  ...
//	                  snapshot:           # added or updated
//	                    ref:    <ref>
//	                    s3Key:  <key>
//	                    bucket: <bucket>
//
// Returns the edited YAML bytes. ErrSiteEntryNotFound is returned if
// no element with `site: <siteKey>` is found anywhere in the matrix.
//
// Tolerant of multiple top-level YAML documents (returns the whole
// stream unchanged except for the matched matrix entry).
func EditApplicationSet(yamlBytes []byte, siteKey string, vals SnapshotValues) ([]byte, error) {
	if siteKey == "" {
		return nil, fmt.Errorf("yamledit: siteKey is empty")
	}

	dec := yaml.NewDecoder(bytes.NewReader(yamlBytes))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("yamledit: decode: %w", err)
		}
		docs = append(docs, &doc)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("yamledit: input contained no YAML documents")
	}

	patched := false
	for _, doc := range docs {
		if patchDoc(doc, siteKey, vals) {
			patched = true
		}
	}
	if !patched {
		return nil, fmt.Errorf("%w: no element with site=%q in applicationset", ErrSiteEntryNotFound, siteKey)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			_ = enc.Close()
			return nil, fmt.Errorf("yamledit: encode: %w", err)
		}
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("yamledit: encoder close: %w", err)
	}
	return buf.Bytes(), nil
}

// ErrSiteEntryNotFound is returned when EditApplicationSet can't find
// a matrix element with `site: <siteKey>`.
var ErrSiteEntryNotFound = errors.New("yamledit: site entry not found")

// patchDoc walks a single YAML document tree and patches any matrix
// element matching siteKey. Returns true if any element was patched.
func patchDoc(doc *yaml.Node, siteKey string, vals SnapshotValues) bool {
	patched := false
	walkMappings(doc, func(m *yaml.Node) {
		// Looking for the inner `elements:` sequence of `list:` blocks.
		// We probe every mapping for an `elements` key whose value is
		// a sequence of mappings; that's where matrix elements live.
		seq := lookupMappingValue(m, "elements")
		if seq == nil || seq.Kind != yaml.SequenceNode {
			return
		}
		for _, elem := range seq.Content {
			if elem.Kind != yaml.MappingNode {
				continue
			}
			siteNode := lookupMappingValue(elem, "site")
			if siteNode == nil || siteNode.Value != siteKey {
				continue
			}
			setSnapshotBlock(elem, vals)
			patched = true
		}
	})
	return patched
}

// setSnapshotBlock adds or updates the `snapshot:` mapping under a
// matrix element node.
func setSnapshotBlock(elem *yaml.Node, vals SnapshotValues) {
	snapMap := lookupMappingValue(elem, "snapshot")
	if snapMap == nil {
		// Add snapshot key+value at end of the element mapping.
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "snapshot"}
		val := newSnapshotMapping(vals)
		elem.Content = append(elem.Content, key, val)
		return
	}
	if snapMap.Kind != yaml.MappingNode {
		// Replace whatever scalar/seq is there with a mapping.
		*snapMap = *newSnapshotMapping(vals)
		return
	}
	setMappingString(snapMap, "ref", vals.Ref)
	setMappingString(snapMap, "s3Key", vals.S3Key)
	setMappingString(snapMap, "bucket", vals.Bucket)
}

func newSnapshotMapping(vals SnapshotValues) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	addMappingString(m, "ref", vals.Ref)
	addMappingString(m, "s3Key", vals.S3Key)
	addMappingString(m, "bucket", vals.Bucket)
	return m
}

func addMappingString(m *yaml.Node, key, val string) {
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val},
	)
}

func setMappingString(m *yaml.Node, key, val string) {
	for i := 0; i < len(m.Content)-1; i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1].Kind = yaml.ScalarNode
			m.Content[i+1].Tag = "!!str"
			m.Content[i+1].Value = val
			m.Content[i+1].Style = 0
			return
		}
	}
	addMappingString(m, key, val)
}

func lookupMappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(m.Content)-1; i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// walkMappings invokes visit for every MappingNode in the tree, top-down.
func walkMappings(n *yaml.Node, visit func(*yaml.Node)) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		visit(n)
	}
	for _, child := range n.Content {
		walkMappings(child, visit)
	}
}
