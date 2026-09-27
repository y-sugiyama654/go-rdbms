// storage/heap/rid.go
package heap

import (
	"fmt"

	"github.com/yourname/go-rdbms/storage/page"
)

// RID (Record ID) はテーブル内のレコードの位置を一意に表す
type RID struct {
	PageID page.PageID
	SlotID uint16
}

func (r RID) String() string {
	return fmt.Sprintf("(%d,%d)", r.PageID, r.SlotID)
}
