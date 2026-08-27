package imap_server

import (
	"errors"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
	"strings"
)

func matchGroup(ctx *context.Context, w *imapserver.ListWriter, basePath, pattern string) error {
	var groups []*models.Group
	if basePath == "" && pattern == "*" {
		if err := db.Instance.Table("group").Where("user_id=?", ctx.UserID).Find(&groups); err != nil {
			log.WithContext(ctx).WithError(err).Error("query IMAP mailbox list failed")
			return listFailedError()
		}
		//w.WriteList(&imap.ListData{
		//	Attrs:   []imap.MailboxAttr{imap.MailboxAttrNoSelect, imap.MailboxAttrHasChildren},
		//	Delim:   '/',
		//	Mailbox: "[Smail]",
		//})
		if err := w.WriteList(&imap.ListData{
			Attrs:   []imap.MailboxAttr{imap.MailboxAttrHasNoChildren},
			Delim:   '/',
			Mailbox: "INBOX",
		}); err != nil {
			return listWriteFailed(ctx, err)
		}
		if err := w.WriteList(&imap.ListData{
			Attrs:   []imap.MailboxAttr{imap.MailboxAttrHasNoChildren, imap.MailboxAttrSent},
			Delim:   '/',
			Mailbox: "Sent Messages",
		}); err != nil {
			return listWriteFailed(ctx, err)
		}
		if err := w.WriteList(&imap.ListData{
			Attrs:   []imap.MailboxAttr{imap.MailboxAttrHasNoChildren, imap.MailboxAttrDrafts},
			Delim:   '/',
			Mailbox: "Drafts",
		}); err != nil {
			return listWriteFailed(ctx, err)
		}

		if err := w.WriteList(&imap.ListData{
			Attrs:   []imap.MailboxAttr{imap.MailboxAttrHasNoChildren, imap.MailboxAttrTrash},
			Delim:   '/',
			Mailbox: "Deleted Messages",
		}); err != nil {
			return listWriteFailed(ctx, err)
		}
		if err := w.WriteList(&imap.ListData{
			Attrs:   []imap.MailboxAttr{imap.MailboxAttrHasNoChildren, imap.MailboxAttrJunk},
			Delim:   '/',
			Mailbox: "Junk",
		}); err != nil {
			return listWriteFailed(ctx, err)
		}
	} else {
		pattern = strings.ReplaceAll(pattern, "/*", "/%")

		if err := db.Instance.Table("group").Where("user_id=? and full_path like ?", ctx.UserID, pattern).Find(&groups); err != nil {
			log.WithContext(ctx).WithError(err).Error("query matching IMAP mailboxes failed")
			return listFailedError()
		}

	}
	for _, group := range groups {

		data := &imap.ListData{
			Attrs:   []imap.MailboxAttr{},
			Mailbox: group.Name,
			Delim:   '/',
		}

		hasChild, err := hasChildren(ctx, group.ID)
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("query IMAP mailbox children failed")
			return listFailedError()
		}
		if hasChild {
			data.Attrs = append(data.Attrs, imap.MailboxAttrHasChildren)
		}

		mailbox, err := getLayerName(ctx, group, true)
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("resolve IMAP mailbox path failed")
			return listFailedError()
		}
		data.Mailbox = mailbox

		if err := w.WriteList(data); err != nil {
			return listWriteFailed(ctx, err)
		}

	}
	return nil
}

func hasChildren(ctx *context.Context, id int) (bool, error) {
	var parent []*models.Group
	err := db.Instance.Table("group").Where("parent_id=? and user_id=?", id, ctx.UserID).Find(&parent)
	return len(parent) > 0, err
}
func getLayerName(ctx *context.Context, item *models.Group, allPath bool) (string, error) {
	return getLayerNameWithVisited(ctx, item, allPath, make(map[int]struct{}))
}

func getLayerNameWithVisited(ctx *context.Context, item *models.Group, allPath bool, visited map[int]struct{}) (string, error) {
	if len(visited) >= 100 {
		return "", errors.New("group hierarchy is too deep")
	}
	if item == nil {
		return "", errors.New("invalid group")
	}
	if item.ID > 0 {
		if _, exists := visited[item.ID]; exists {
			return "", errors.New("group hierarchy cycle detected")
		}
		visited[item.ID] = struct{}{}
	}
	if item.ParentId == 0 {
		return item.Name, nil
	}
	var parent models.Group
	found, err := db.Instance.Table("group").Where("id=? and user_id=?", item.ParentId, ctx.UserID).Get(&parent)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("group parent not found")
	}
	parentName, err := getLayerNameWithVisited(ctx, &parent, allPath, visited)
	if err != nil {
		return "", err
	}
	if allPath {
		return parentName + "/" + item.Name, nil
	}
	return parentName, nil
}

func (s *serverSession) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	log.WithContext(s.ctx).Debugf("IMAP LIST started: pattern_count=%d", len(patterns))

	if ref == "" && len(patterns) == 0 {
		if err := w.WriteList(&imap.ListData{
			Attrs:   []imap.MailboxAttr{imap.MailboxAttrNoSelect, imap.MailboxAttrHasChildren},
			Delim:   '/',
			Mailbox: "[Smail]",
		}); err != nil {
			return listWriteFailed(s.ctx, err)
		}
	}
	for _, pattern := range patterns {
		if err := matchGroup(s.ctx, w, ref, pattern); err != nil {
			return err
		}
	}

	return nil
}

func listWriteFailed(ctx *context.Context, err error) error {
	log.WithContext(ctx).WithError(err).Error("write IMAP LIST response failed")
	return listFailedError()
}

func listFailedError() error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "List failed"}
}
