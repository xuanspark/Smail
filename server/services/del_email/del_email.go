package del_email

import (
	"fmt"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"log/slog"
	"xorm.io/xorm"
)
import . "xorm.io/builder"

func DelEmail(ctx *context.Context, ids []int, forcedDel bool) error {
	session := db.Instance.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return err
	}
	for _, id := range ids {
		err := deleteOne(ctx, session, cast.ToInt64(id), forcedDel)
		if err != nil {
			rollbackDeleteSession(session)
			return err
		}
	}
	return session.Commit()
}

type num struct {
	Num int `xorm:"num"`
}

func deleteOne(ctx *context.Context, session *xorm.Session, id int64, forcedDel bool) error {
	if !forcedDel {
		if _, err := session.Table(&models.UserEmail{}).Where("email_id=? and user_id=?", id, ctx.UserID).Update(map[string]interface{}{
			"status":   consts.EmailStatusDel,
			"group_id": 0,
		}); err != nil {
			return fmt.Errorf("mark email deleted: %w", err)
		}
		return nil
	}

	var ue models.UserEmail
	found, err := session.Table(&models.UserEmail{}).Where("email_id=? and user_id=?", id, ctx.UserID).Get(&ue)
	if err != nil {
		return fmt.Errorf("load email relation for deletion: %w", err)
	}
	if !found || ue.ID == 0 {
		return nil
	}

	affected, err := session.Table(&models.UserEmail{}).Where("id=? and user_id=?", ue.ID, ctx.UserID).Delete(&models.UserEmail{})
	if err != nil {
		return fmt.Errorf("delete email relation: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("delete email relation affected %d rows", affected)
	}

	remaining, err := session.Where("email_id=?", id).Count(&models.UserEmail{})
	if err != nil {
		return fmt.Errorf("count remaining email relations: %w", err)
	}
	if remaining == 0 {
		if _, err := session.Table(&models.Email{}).Where("id=?", id).Delete(&models.Email{}); err != nil {
			return fmt.Errorf("delete unreferenced email: %w", err)
		}
	}
	return nil
}
func DelByUID(ctx *context.Context, ids []int) error {
	session := db.Instance.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return err
	}
	for _, id := range ids {
		var ue models.UserEmail
		found, err := session.Table("user_email").Where(Eq{"id": id, "user_id": ctx.UserID}).Get(&ue)
		if err != nil {
			rollbackDeleteSession(session)
			return err
		}
		if !found || ue.ID == 0 {
			log.WithContext(ctx).Warn("no user email found")
			continue
		}
		emailId := ue.EmailID

		// 先删除关联关系
		_, err = session.Table(&models.UserEmail{}).Where("id=? and user_id=?", id, ctx.UserID).Delete(&ue)
		if err != nil {
			slog.Error("SQLError", slog.Any("err", err))
			rollbackDeleteSession(session)
			return err
		}

		// 检查email是否还有人有权限
		var Num num
		_, err = session.Table(&models.UserEmail{}).Select("count(1) as num").Where("email_id=? ", emailId).Get(&Num)
		if err != nil {
			slog.Error("SQLError", slog.Any("err", err))
			rollbackDeleteSession(session)
			return err
		}
		if Num.Num == 0 {
			var email models.Email
			_, err = session.Table(&email).Where("id=?", emailId).Delete(&email)
			if err != nil {
				slog.Error("SQLError", slog.Any("err", err))
				rollbackDeleteSession(session)
				return err
			}
		}
	}
	return session.Commit()
}

func rollbackDeleteSession(session *xorm.Session) {
	if err := session.Rollback(); err != nil {
		slog.Error("rollback email delete transaction failed", slog.Any("err", err))
	}
}
