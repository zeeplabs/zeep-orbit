package orbit

import (
	"fmt"
	"net/url"
)

type TableClient struct {
	client *Client
	table  string
}

func (t *TableClient) path(id string) string {
	if id == "" {
		return fmt.Sprintf("%s/", t.table)
	}
	return fmt.Sprintf("%s/%s/", t.table, id)
}

type FindManyParams struct {
	Limit   int
	Offset  int
	Order   string
	Filters map[string]string
}

func (t *TableClient) FindMany(params *FindManyParams) (*ListResponse, error) {
	q := url.Values{}
	if params != nil {
		if params.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", params.Limit))
		}
		if params.Offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", params.Offset))
		}
		if params.Order != "" {
			q.Set("order", params.Order)
		}
		for k, v := range params.Filters {
			q.Set(k, v)
		}
	}
	path := t.table
	if len(q) > 0 {
		path += "/?" + q.Encode()
	}
	var resp ListResponse
	err := t.client.request("GET", path, nil, &resp)
	return &resp, err
}

func (t *TableClient) FindByID(id string) (map[string]any, error) {
	var row map[string]any
	err := t.client.request("GET", t.path(id), nil, &row)
	return row, err
}

func (t *TableClient) Create(data map[string]any) (map[string]any, error) {
	var row map[string]any
	err := t.client.request("POST", t.table+"/", data, &row)
	return row, err
}

func (t *TableClient) Update(id string, data map[string]any) (map[string]any, error) {
	var row map[string]any
	err := t.client.request("PATCH", t.path(id), data, &row)
	return row, err
}

func (t *TableClient) Replace(id string, data map[string]any) (map[string]any, error) {
	var row map[string]any
	err := t.client.request("PUT", t.path(id), data, &row)
	return row, err
}

func (t *TableClient) Delete(id string) error {
	return t.client.request("DELETE", t.path(id), nil, nil)
}
