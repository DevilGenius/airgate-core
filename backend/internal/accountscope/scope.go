package accountscope

import (
	"errors"

	"github.com/DevilGenius/airgate-core/ent"
	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/ent/predicate"
)

// ErrAccountNotFound 统一表示账号不存在或已被软删除。
// 账号应用层与直接操作 Ent 的调度状态写入共享此错误，避免相同竞态返回不同语义。
var ErrAccountNotFound = errors.New("账号不存在")

// NormalizeNotFoundError 将 Ent 的未找到错误收敛为账号作用域的统一领域错误。
func NormalizeNotFoundError(err error) error {
	if ent.IsNotFound(err) {
		return ErrAccountNotFound
	}
	return err
}

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
