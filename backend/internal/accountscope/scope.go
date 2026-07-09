package accountscope

import (
	"github.com/DevilGenius/airgate-core/ent"
	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/ent/predicate"
)

// NotDeleted 返回普通业务查询使用的账号作用域。
// Usage Log 等历史回溯路径不应使用此谓词，以便继续加载软删除账号。
func NotDeleted() predicate.Account {
	return entaccount.DeletedAtIsNil()
}

// Query 创建仅包含未删除账号的查询。
func Query(db *ent.Client) *ent.AccountQuery {
	return db.Account.Query().Where(NotDeleted())
}

// QueryByID 创建按 ID 查询未删除账号的查询。
func QueryByID(db *ent.Client, id int) *ent.AccountQuery {
	return Query(db).Where(entaccount.IDEQ(id))
}

// UpdateOneID 创建仅允许修改未删除账号的单行更新。
func UpdateOneID(db *ent.Client, id int) *ent.AccountUpdateOne {
	return db.Account.UpdateOneID(id).Where(NotDeleted())
}
