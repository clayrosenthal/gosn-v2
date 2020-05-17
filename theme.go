package gosn

import (
	"fmt"
	"time"
)

type Theme struct {
	ItemCommon
	Content ThemeContent
}

func (i Items) Themes() (c Themes) {
	for _, x := range i {
		if x.GetContentType() == "Theme" {
			component := x.(*Theme)
			c = append(c, *component)
		}
	}

	return c
}

func (c *Themes) DeDupe() {
	var encountered []string

	var deDuped Themes

	for _, i := range *c {
		if !stringInSlice(i.UUID, encountered, true) {
			deDuped = append(deDuped, i)
		}

		encountered = append(encountered, i.UUID)
	}

	*c = deDuped
}

// NewTheme returns an Item of type Theme without content
func NewTheme() Theme {
	now := time.Now().UTC().Format(timeLayout)

	var c Theme

	c.ContentType = "SN|Theme"
	c.CreatedAt = now
	c.UpdatedAt = now
	c.UUID = GenUUID()

	return c
}

// NewTagContent returns an empty Tag content instance
func NewThemeContent() *ThemeContent {
	c := &ThemeContent{}
	c.SetUpdateTime(time.Now().UTC())

	return c
}

type Themes []Theme

func (c Themes) Validate() error {
	var updatedTime time.Time

	var err error

	for _, item := range c {
		// validate content if being added
		if !item.Deleted {
			updatedTime, err = item.Content.GetUpdateTime()
			if err != nil {
				return err
			}

			switch {
			case item.Content.Name == "":
				err = fmt.Errorf("failed to create \"%s\" due to missing title: \"%s\"",
					item.ContentType, item.UUID)
			case updatedTime.IsZero():
				err = fmt.Errorf("failed to create \"%s\" due to missing content updated time: \"%s\"",
					item.ContentType, item.Content.GetTitle())
			case item.CreatedAt == "":
				err = fmt.Errorf("failed to create \"%s\" due to missing created at date: \"%s\"",
					item.ContentType, item.Content.GetTitle())
			}

			if err != nil {
				return err
			}
		}
	}

	return err
}

func (c Theme) IsDeleted() bool {
	return c.Deleted
}

func (c *Theme) SetDeleted(d bool) {
	c.Deleted = d
}

func (c Theme) GetContent() Content {
	return &c.Content
}

func (c *Theme) SetContent(cc Content) {
	c.Content = cc.(ThemeContent)
}

func (c Theme) GetUUID() string {
	return c.UUID
}

func (c *Theme) SetUUID(u string) {
	c.UUID = u
}

func (c Theme) GetContentType() string {
	return c.ContentType
}

func (c Theme) GetCreatedAt() string {
	return c.CreatedAt
}

func (c *Theme) SetCreatedAt(ca string) {
	c.CreatedAt = ca
}

func (c Theme) GetUpdatedAt() string {
	return c.UpdatedAt
}

func (c *Theme) SetUpdatedAt(ca string) {
	c.UpdatedAt = ca
}

func (c *Theme) SetContentType(ct string) {
	c.ContentType = ct
}

func (c Theme) GetContentSize() int {
	return c.ContentSize
}

func (c *Theme) SetContentSize(s int) {
	c.ContentSize = s
}

func (cc *ThemeContent) AssociateItems(newItems []string) {
	// add to associated item ids
	for _, newRef := range newItems {
		var existingFound bool

		var existingDFound bool

		for _, existingRef := range cc.AssociatedItemIds {
			if existingRef == newRef {
				existingFound = true
			}
		}

		for _, existingDRef := range cc.DissociatedItemIds {
			if existingDRef == newRef {
				existingDFound = true
			}
		}

		// add reference if it doesn't exist
		if !existingFound {
			cc.AssociatedItemIds = append(cc.AssociatedItemIds, newRef)
		}

		// remove reference (from disassociated) if it does exist in that list
		if existingDFound {
			cc.DissociatedItemIds = removeStringFromSlice(newRef, cc.DissociatedItemIds)
		}
	}
}

func (cc *ThemeContent) GetItemAssociations() []string {
	return cc.AssociatedItemIds
}

func (cc *ThemeContent) GetItemDisassociations() []string {
	return cc.DissociatedItemIds
}

func (cc *ThemeContent) DisassociateItems(itemsToRemove []string) {
	// remove from associated item ids
	for _, delRef := range itemsToRemove {
		var existingFound bool

		for _, existingRef := range cc.AssociatedItemIds {
			if existingRef == delRef {
				existingFound = true
			}
		}

		// remove reference (from disassociated) if it does exist in that list
		if existingFound {
			cc.AssociatedItemIds = removeStringFromSlice(delRef, cc.AssociatedItemIds)
		}
	}
}

func (cc *ThemeContent) GetUpdateTime() (time.Time, error) {
	if cc.AppData.OrgStandardNotesSN.ClientUpdatedAt == "" {
		return time.Time{}, fmt.Errorf("notset")
	}

	return time.Parse(timeLayout, cc.AppData.OrgStandardNotesSN.ClientUpdatedAt)
}

func (cc *ThemeContent) SetUpdateTime(uTime time.Time) {
	cc.AppData.OrgStandardNotesSN.ClientUpdatedAt = uTime.Format(timeLayout)
}

func (cc ThemeContent) GetTitle() string {
	return ""
}

func (cc *ThemeContent) GetName() string {
	return cc.Name
}

func (cc *ThemeContent) GetActive() bool {
	return cc.Active.(bool)
}

func (cc *ThemeContent) SetTitle(title string) {
}

func (cc *ThemeContent) GetAppData() AppDataContent {
	return cc.AppData
}

func (cc *ThemeContent) SetAppData(data AppDataContent) {
	cc.AppData = data
}

func (cc ThemeContent) References() ItemReferences {
	return cc.ItemReferences
}

func (cc *ThemeContent) UpsertReferences(input ItemReferences) {
	panic("implement me")
}

func (cc *ThemeContent) SetReferences(input ItemReferences) {
	panic("implement me")
}
