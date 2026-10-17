package upstream

import (
	"context"
	"time"
)

type BookCollection struct {
	Name  string `json:"name"`
	Books []Book `json:"books"`
}

type BookCollectionPage struct {
	Items           []BookCollection `json:"items"`
	Page            int              `json:"page"`
	PageSize        int              `json:"pageSize"`
	HasMore         bool             `json:"hasMore"`
	Source          string           `json:"source"`
	FetchedAt       time.Time        `json:"fetchedAt"`
	ExpiresAt       time.Time        `json:"expiresAt"`
	ReferenceStatus string           `json:"referenceStatus"`
}

// Collections come only from filtered, personally accessible catalog records.
// Grimmory's series navigation does not apply content restrictions in v3.5.0.
func (c *Client) GrimmoryCollections(ctx context.Context, base, publicBase string, credentials BookCredentials, series string, page int) (BookCollectionPage, error) {
	books, err := c.grimmoryBooks(ctx, base, publicBase, credentials, "", series, page)
	if err != nil {
		return BookCollectionPage{}, err
	}
	now := time.Now().UTC()
	result := BookCollectionPage{Items: []BookCollection{}, Page: books.Page, PageSize: books.PageSize, HasMore: books.HasMore, Source: "grimmory_opds", FetchedAt: now, ExpiresAt: now.Add(time.Minute), ReferenceStatus: "unavailable"}
	positions := map[string]int{}
	for _, book := range books.Items {
		if book.Series == "" {
			continue
		}
		index, exists := positions[book.Series]
		if !exists {
			index = len(result.Items)
			positions[book.Series] = index
			result.Items = append(result.Items, BookCollection{Name: book.Series, Books: []Book{}})
		}
		result.Items[index].Books = append(result.Items[index].Books, book)
	}
	return result, nil
}
