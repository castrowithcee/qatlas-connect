package twentycrm

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

const (
	// activityPageSize and activityMaxPages bound the metadata read that carries the active state of fields.
	activityPageSize = 50
	activityMaxPages = 20
)

// activityRecord is the part of an object of the metadata API this file reads.
type activityRecord struct {
	NameSingular string `json:"nameSingular"`
	Fields       []struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		IsActive *bool  `json:"isActive"`
	} `json:"fields"`
}

// inactive reports whether Twenty marks the field inactive. An unknown state counts as active.
func (f catalogField) inactive() bool {
	return f.Active != nil && !*f.Active
}

// activeCatalog is workspaceCatalog plus the active state of every field, read at most once per client.
// The generated workspace document lists inactive fields like active ones. The state comes from the
// metadata API, which not every key may read: any failure of that read leaves the state unknown, which
// behaves as before, and is not retried.
func (c *Client) activeCatalog(ctx context.Context, op string) (*catalog, error) {
	cat, err := c.workspaceCatalog(ctx, op)
	if err != nil {
		return nil, err
	}
	if c.activityRead {
		return cat, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, providerError(op, "the request ended before the field states were read")
	}
	c.activityRead = true
	cat.applyActivity(c.readActivity(ctx))
	return cat, nil
}

// readActivity pages through the object metadata. A failed or unusable read yields nothing at all.
func (c *Client) readActivity(ctx context.Context) []activityRecord {
	const op = "read field states"
	var records []activityRecord
	after := ""
	for page := 0; page < activityMaxPages; page++ {
		values := url.Values{"limit": {strconv.Itoa(activityPageSize)}}
		if after != "" {
			values.Set("starting_after", after)
		}
		var body metaPage
		if c.getWith(ctx, op, metadataObjectsPath, values, metadataResponseBytes, &body, metaStatusTexts) != nil {
			return nil
		}
		items, err := body.items(op, activityPageSize)
		if err != nil {
			return nil
		}
		for _, item := range items {
			var record activityRecord
			if json.Unmarshal(item, &record) != nil {
				return nil
			}
			records = append(records, record)
		}
		if !body.PageInfo.HasNextPage {
			return records
		}
		if body.PageInfo.EndCursor == nil || !validUUID(*body.PageInfo.EndCursor) {
			return nil
		}
		after = *body.PageInfo.EndCursor
	}
	return nil
}

// applyActivity records the active state on the catalog fields that the metadata names. The identifier field
// of an inactive relation (<name>Id) follows its relation.
func (cat *catalog) applyActivity(records []activityRecord) {
	for _, record := range records {
		object := cat.objects[record.NameSingular]
		if object == nil {
			continue
		}
		index := make(map[string]*catalogField, len(object.Fields))
		for i := range object.Fields {
			index[object.Fields[i].Name] = &object.Fields[i]
		}
		for _, meta := range record.Fields {
			field := index[meta.Name]
			if field == nil || meta.IsActive == nil {
				continue
			}
			active := *meta.IsActive
			field.Active = &active
			if active || (meta.Type != "RELATION" && meta.Type != "MORPH_RELATION") {
				continue
			}
			if id := index[meta.Name+"Id"]; id != nil && id.Active == nil {
				id.Active = &active
			}
		}
	}
}
