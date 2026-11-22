// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

import "encoding/json"

type found struct {
	source  *widgetSource
	exports []exportDoc
}

func (b *builder) generic() (Plan, error) {
	f, err := b.scan()
	if err != nil {
		return Plan{}, err
	}
	for _, e := range b.entries {
		if !b.used[e.Path] {
			b.registerEntry(e)
		}
	}
	if err := b.apply(f); err != nil {
		return Plan{}, err
	}
	return b.plan(), nil
}

func (b *builder) scan() (found, error) {
	var f found
	src, ok, err := b.readWidgetFolder()
	if err != nil {
		return f, err
	}
	if ok {
		f.source = &src
	}
	f.exports, err = b.readExports()
	return f, err
}

func (b *builder) apply(f found) error {
	var err error
	if f.source != nil {
		err = b.addFolderWidget(*f.source)
	}
	if err != nil {
		return err
	}
	for _, doc := range f.exports {
		if err = b.addExport(doc); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) readExports() ([]exportDoc, error) {
	var docs []exportDoc
	for _, e := range b.entries {
		if b.used[e.Path] || !isJSON(e.Path) {
			continue
		}
		doc, ok, err := b.readExport(e)
		if err != nil {
			return nil, err
		}
		if ok {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

func (b *builder) readExport(e entry) (exportDoc, bool, error) {
	data, within, err := e.read(exportLimit)
	if err != nil {
		return exportDoc{}, false, err
	}
	var doc exportDoc
	if !within || json.Unmarshal(data, &doc) != nil {
		return exportDoc{}, false, nil
	}
	b.used[e.Path] = len(doc.Widgets) > 0
	return doc, len(doc.Widgets) > 0, nil
}
