package imap_server

import (
	"fmt"

	"git.pcux.cn/xuans/Smail/services/list"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

// Search implements the IMAP SEARCH command with full criteria support
// Supports: UID, SeqNum, Date filters, Header search, Body/Text search,
// Flag filters, Size filters, and logical combinations (NOT, OR)
func (s *serverSession) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	log.WithContext(s.ctx).Debugf("IMAP SEARCH started: kind=%v", kind)

	// Use the new comprehensive search function
	uids, err := s.selectedUIDs()
	if err != nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Search failed"}
	}
	retList, err := list.SearchEmails(s.ctx, s.currentMailbox, criteria, uids)
	if err != nil {
		log.WithContext(s.ctx).Errorf("IMAP SEARCH error: %v", err)
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Search failed"}
	}

	ret := &imap.SearchData{}

	if kind == imapserver.NumKindSeq {
		// Return sequence numbers
		idList := imap.SeqSet{}
		for _, data := range retList {
			sequence, err := imapUint32(data.SerialNumber)
			if err != nil || sequence == 0 {
				return nil, fmt.Errorf("IMAP sequence number out of range")
			}
			idList = append(idList, imap.SeqRange{
				Start: sequence,
				Stop:  sequence,
			})
		}
		count, err := imapUint32(len(retList))
		if err != nil {
			return nil, err
		}
		ret.All = idList
		ret.Count = count

		// Handle ESEARCH options
		if options != nil && len(idList) > 0 {
			if options.ReturnMin {
				ret.Min = idList[0].Start
				for _, item := range idList[1:] {
					if item.Start < ret.Min {
						ret.Min = item.Start
					}
				}
			}
			if options.ReturnMax {
				ret.Max = idList[0].Start
				for _, item := range idList[1:] {
					if item.Start > ret.Max {
						ret.Max = item.Start
					}
				}
			}
		}
	} else {
		// Return UIDs
		idList := imap.UIDSet{}
		for _, data := range retList {
			uid, err := imapUID(data.ID)
			if err != nil {
				return nil, err
			}
			idList = append(idList, imap.UIDRange{
				Start: uid,
				Stop:  uid,
			})
		}
		count, err := imapUint32(len(retList))
		if err != nil {
			return nil, err
		}
		ret.UID = true
		ret.All = idList
		ret.Count = count

		// Handle ESEARCH options
		if options != nil && len(idList) > 0 {
			if options.ReturnMin {
				ret.Min = cast.ToUint32(idList[0].Start)
				for _, item := range idList[1:] {
					value := cast.ToUint32(item.Start)
					if value < ret.Min {
						ret.Min = value
					}
				}
			}
			if options.ReturnMax {
				ret.Max = cast.ToUint32(idList[0].Start)
				for _, item := range idList[1:] {
					value := cast.ToUint32(item.Start)
					if value > ret.Max {
						ret.Max = value
					}
				}
			}
		}
	}

	log.WithContext(s.ctx).Debugf("IMAP SEARCH result: count=%d", ret.Count)
	return ret, nil
}
