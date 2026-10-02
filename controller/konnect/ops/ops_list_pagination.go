package ops

import (
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	"github.com/kong/kong-operator/v2/internal/utils/konnectpagination"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// listPageSize is the page size requested when listing Konnect entities page
// by page, e.g. to find an entity by its Kubernetes UID. It defaults to
// consts.DefaultKonnectListPageSize and is set once at startup, before any
// controller runs, with SetListPageSize (the --konnect-list-page-size flag).
var listPageSize int64 = consts.DefaultKonnectListPageSize

// SetListPageSize sets the page size requested when listing Konnect entities
// page by page. It must be called before the controllers start, as it is not
// safe for concurrent use; size must be between 1 and
// consts.MaxKonnectListPageSize.
func SetListPageSize(size int64) error {
	if size < 1 || size > consts.MaxKonnectListPageSize {
		return fmt.Errorf("page size for Konnect lists must be between 1 and %d, got %d", consts.MaxKonnectListPageSize, size)
	}
	listPageSize = size
	return nil
}

// nextPageCursor returns the page[after] cursor of the page following the one
// whose meta.page.next is next, or nil when that page was the last one.
// seenCursors holds the cursors already requested: a cursor seen again is
// reported as an error, as following it would never end the listing.
func nextPageCursor(next *string, seenCursors map[string]struct{}) (*string, error) {
	if next == nil || *next == "" {
		return nil, nil
	}
	cursor, err := konnectpagination.PageAfterCursorFromNextPageURL(*next)
	if err != nil {
		return nil, err
	}
	if _, ok := seenCursors[cursor]; ok {
		return nil, fmt.Errorf("next page cursor %q repeated", cursor)
	}
	seenCursors[cursor] = struct{}{}
	return &cursor, nil
}

// hasNextNumberedPage reports whether a page-number paginated listing has a
// page after page pageNumber, whose meta.page is page and which returned items
// items. An empty page ends the listing even when the total says otherwise, so
// that a total that never matches the items cannot page forever.
func hasNextNumberedPage(pageNumber int64, page sdkkonnectcomp.PageMeta, items int) bool {
	if items == 0 || page.GetSize() <= 0 {
		return false
	}
	return float64(pageNumber)*page.GetSize() < page.GetTotal()
}
