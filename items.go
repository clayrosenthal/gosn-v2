package gosn

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh/terminal"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

type syncResponse struct {
	Items        EncryptedItems  `json:"retrieved_items"`
	SavedItems   EncryptedItems  `json:"saved_items"`
	Unsaved      EncryptedItems  `json:"unsaved"`
	Conflicts    ConflictedItems `json:"conflicts"`
	SyncToken    string          `json:"sync_token"`
	CursorToken  string          `json:"cursor_token"`
	LastItemPut  int             // the last item successfully put
	PutLimitUsed int             // the put limit used
}

// AppTagConfig defines expected configuration structure for making Tag related operations.
type AppTagConfig struct {
	Email    string
	Token    string
	FindText string
	FindTag  string
	NewTags  []string
	Debug    bool
}

const retryScaleFactor = 0.25

type EncryptedItems []EncryptedItem

func (ei EncryptedItems) DecryptAndParseItemsKeys(mk string, debug bool) (o []ItemsKey, err error) {
	debugPrint(debug, fmt.Sprintf("DecryptAndParseItemsKeys | encrypted items to check: %d", len(ei)))

	if len(ei) == 0 {
		return
	}

	var eiks EncryptedItems

	for _, e := range ei {
		if e.ContentType == "SN|ItemsKey" && !e.Deleted {
			if e.UUID == "" {
				panic("DecryptAndParseItemsKeys | items key has no uuid")
			}

			if e.EncItemKey == "" {
				panic(fmt.Sprintf("DecryptAndParseItemsKeys | items key uuid: %s has no encrypted item key", e.UUID))
			}

			eiks = append(eiks, e)
		}
	}

	if len(eiks) == 0 {
		// err = fmt.Errorf("no items keys were retrieved")

		return
	}

	o, err = DecryptAndParseItemKeys(mk, eiks)
	if err != nil {
		err = fmt.Errorf("gsDecrypt | %w", err)

		return
	}

	if len(o) == 0 {
		err = fmt.Errorf("failed to decrypt and parse items keys")
		return
	}

	return
}

func IsEncryptedType(ct string) bool {
	switch {
	case strings.HasPrefix(ct, "SF"):
		return false
	case ct == "SN|ItemsKey":
		return false
	default:
		return true
	}
}

func (ei *EncryptedItems) Validate() error {
	var err error

	dei := *ei

	for x := range dei {
		enc := IsEncryptedType(dei[x].ContentType)

		switch {
		case dei[x].IsDeleted():
			continue
		case enc && dei[x].ItemsKeyID == nil:
			// ignore item in this scenario as the official app does so
		case enc && dei[x].EncItemKey == "":
			err = fmt.Errorf("validation failed for \"%s\" due to missing encrypted item key: \"%s\"",
				dei[x].ContentType, dei[x].UUID)
		}

		if err != nil {
			return err
		}
	}

	return err
}

func (ei EncryptedItems) ReEncrypt(s *Session, decryptionItemsKey ItemsKey, newItemsKey ItemsKey, newMasterKey string) (o EncryptedItems, err error) {
	debugPrint(s.Debug, fmt.Sprintf("ReEncrypt | items: %d", len(ei)))

	var di DecryptedItems

	di, err = ei.Decrypt(s, decryptionItemsKey)

	if err != nil {
		err = fmt.Errorf("DecryptAndParse | Decrypt | %w", err)
		return
	}

	var itemsToEncrypt Items

	itemsToEncrypt, err = di.Parse()
	if err != nil {
		err = fmt.Errorf("DecryptAndParse | Parse | %w", err)

		return
	}

	return itemsToEncrypt.Encrypt(newItemsKey, newMasterKey, s.Debug)
}

func (ei EncryptedItems) DecryptAndParse(s *Session) (o Items, err error) {
	debugPrint(s.Debug, fmt.Sprintf("DecryptAndParse | items: %d", len(ei)))

	var di DecryptedItems

	if s.ImporterItemsKey.ItemsKey != "" {
		debugPrint(s.Debug, fmt.Sprintf("DecryptAndParse | using ImportersItemsKey"))
		di, err = ei.Decrypt(s, s.ImporterItemsKey)
	} else {
		di, err = ei.Decrypt(s, ItemsKey{})
	}

	if err != nil {
		err = fmt.Errorf("DecryptAndParse | Decrypt | %w", err)
		return
	}

	o, err = di.Parse()
	if err != nil {
		err = fmt.Errorf("DecryptAndParse | Parse | %w", err)

		return
	}

	return
}

func (i *Items) Append(x []interface{}) {
	var all Items

	for _, y := range x {
		switch t := y.(type) {
		case Note:
			it := t
			all = append(all, &it)
		case Tag:
			it := t
			all = append(all, &it)
		case Component:
			it := t
			all = append(all, &it)
		}
	}

	*i = all
}

func (i *Items) Encrypt(ik ItemsKey, masterKey string, debug bool) (e EncryptedItems, err error) {
	// return empty if no items provided
	if len(*i) == 0 {
		return
	}

	e, err = encryptItems(i, ik, masterKey, debug)
	if err != nil {
		return
	}

	for x := range e {
		if e[x].ContentType == "SN|ItemsKey" {
			panic("pantyhose")
		}
	}

	if err = e.Validate(); err != nil {
		return e, err
	}

	return
}

type EncryptedItem struct {
	UUID        string  `json:"uuid"`
	ItemsKeyID  *string `json:"items_key_id,omitempty"`
	Content     string  `json:"content"`
	ContentType string  `json:"content_type"`
	EncItemKey  string  `json:"enc_item_key"`
	Deleted     bool    `json:"deleted"`
	// Default            bool    `json:"isDefault"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	CreatedAtTimestamp int64   `json:"created_at_timestamp"`
	UpdatedAtTimestamp int64   `json:"updated_at_timestamp"`
	DuplicateOf        *string `json:"duplicate_of,omitempty"`
}

func (ei EncryptedItem) GetItemsKeyID() string {
	return *ei.ItemsKeyID
}

func (ei EncryptedItem) IsDeleted() bool {
	return ei.Deleted
}

type DecryptedItem struct {
	UUID               string `json:"uuid"`
	ItemsKeyID         string `json:"items_key_id,omitempty"`
	Content            string `json:"content"`
	ContentType        string `json:"content_type"`
	Deleted            bool   `json:"deleted"`
	Default            bool   `json:"isDefault"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	CreatedAtTimestamp int64  `json:"created_at_timestamp"`
	UpdatedAtTimestamp int64  `json:"updated_at_timestamp"`
}

type DecryptedItems []DecryptedItem

type UpdateItemRefsInput struct {
	Items Items // Tags
	ToRef Items // Items To Reference
}

type UpdateItemRefsOutput struct {
	Items Items // Tags
}

func UpdateItemRefs(i UpdateItemRefsInput) UpdateItemRefsOutput {
	var updated Items // updated tags

	for _, item := range i.Items {
		var refs ItemReferences

		for _, tr := range i.ToRef {
			ref := ItemReference{
				UUID:        tr.GetUUID(),
				ContentType: tr.GetContentType(),
			}
			refs = append(refs, ref)
		}

		switch item.GetContent().(type) {
		case *NoteContent:
			ic := item.GetContent().(*NoteContent)
			ic.UpsertReferences(refs)
			item.SetContent(*ic)
		case *TagContent:
			ic := item.GetContent().(*TagContent)
			ic.UpsertReferences(refs)
			item.SetContent(*ic)
		}

		updated = append(updated, item)
	}

	return UpdateItemRefsOutput{
		Items: updated,
	}
}

func makeSyncRequest(session Session, reqBody []byte) (responseBody []byte, err error) {
	var request *http.Request

	request, err = http.NewRequest(http.MethodPost, session.Server+syncPath, bytes.NewBuffer(reqBody))
	if err != nil {
		return
	}

	request.Header.Set("content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)
	request.Header.Set("User-Agent", "github.com/jonhadfield/gosn-v2")

	var response *http.Response

	start := time.Now()
	response, err = httpClient.Do(request)
	elapsed := time.Since(start)

	debugPrint(session.Debug, fmt.Sprintf("makeSyncRequest | request took: %v", elapsed))

	if err != nil {
		return
	}

	defer func() {
		if err := response.Body.Close(); err != nil {
			debugPrint(session.Debug, "makeSyncRequest | failed to close body closed")
		}
	}()

	if response.StatusCode == 413 {
		err = errors.New("payload too large")
		return
	}

	if response.StatusCode == 498 {
		err = errors.New("session token is invalid or has expired")
		return
	}

	if response.StatusCode == 401 {
		debugPrint(session.Debug, fmt.Sprintf("makeSyncRequest | sync of %d req bytes failed with: %s", len(reqBody), response.Status))

		err = errors.New("server returned 401 unauthorized during sync request so most likely throttling due to excessive number of requests")

		return
	}

	if response.StatusCode > 400 {
		debugPrint(session.Debug, fmt.Sprintf("makeSyncRequest | sync of %d req bytes failed with: %s", len(reqBody), response.Status))
		return
	}

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		debugPrint(session.Debug, fmt.Sprintf("makeSyncRequest | sync of %d req bytes succeeded with: %s", len(reqBody), response.Status))
	}

	// readStart := time.Now()

	responseBody, err = ioutil.ReadAll(response.Body)

	// debugPrint(session.Debug, fmt.Sprintf("makeSyncRequest | response read took %+v", time.Since(readStart)))

	return responseBody, err
}

// ItemReference defines a reference from one item to another.
type ItemReference struct {
	// unique identifier of the item being referenced
	UUID string `json:"uuid"`
	// type of item being referenced
	ContentType string `json:"content_type"`
}

type OrgStandardNotesSNDetail struct {
	ClientUpdatedAt    string `json:"client_updated_at"`
	PrefersPlainEditor bool   `json:"prefersPlainEditor"`
}

type AppDataContent struct {
	OrgStandardNotesSN OrgStandardNotesSNDetail `json:"org.standardnotes.sn"`
}

type TagContent struct {
	Title          string         `json:"title"`
	ItemReferences ItemReferences `json:"references"`
	AppData        AppDataContent `json:"appData"`
}

func removeStringFromSlice(inSt string, inSl []string) (outSl []string) {
	for _, si := range inSl {
		if inSt != si {
			outSl = append(outSl, si)
		}
	}

	return
}

type ItemReferences []ItemReference

type Items []Item

func (di *DecryptedItems) Parse() (p Items, err error) {
	for _, i := range *di {
		var pi Item

		switch i.ContentType {
		case "SN|ItemsKey":
			// TODO: To be implemented separately so we don't parse as a normal item and,
			// most importantly, don't return as a normal Item
			continue
		case "Note":
			pi = parseNote(i)
		case "Tag":
			pi = parseTag(i)
		case "SN|Component":
			pi = parseComponent(i)
		case "SN|Theme":
			pi = parseTheme(i)
		case "SN|Privileges":
			pi = parsePrivileges(i)
		case "Extension":
			pi = parseExtension(i)
		case "SF|Extension":
			pi = parseSFExtension(i)
		case "SF|MFA":
			pi = parseSFMFA(i)
		case "SN|SmartTag":
			pi = parseSmartTag(i)
		case "SN|FileSafe|FileMetadata":
			pi = parseFileSafeFileMetadata(i)
		case "SN|FileSafe|Integration":
			pi = parseFileSafeIntegration(i)
		case "SN|UserPreferences":
			pi = parseUserPreferences(i)
		case "SN|ExtensionRepo":
			pi = parseExtensionRepo(i)
		case "SN|FileSafe|Credentials":
			pi = parseFileSafeCredentials(i)
		default:
			return nil, fmt.Errorf("unhandled type '%s'", i.ContentType)
		}

		p = append(p, pi)
	}

	return p, err
}

func processContentModel(contentType, input string) (output Content, err error) {
	// identify content model
	// try and unmarshall Item
	switch contentType {
	case "Note":
		var nc NoteContent
		err = json.Unmarshal([]byte(input), &nc)

		return nc, err
	case "Tag":
		var tc TagContent
		err = json.Unmarshal([]byte(input), &tc)

		return tc, err
	case "SN|Component":
		var cc ComponentContent
		err = json.Unmarshal([]byte(input), &cc)

		return cc, err
	case "SN|Theme":
		var tc ThemeContent
		err = json.Unmarshal([]byte(input), &tc)

		return tc, err
	case "SN|Privileges":
		var pc PrivilegesContent
		err = json.Unmarshal([]byte(input), &pc)

		return pc, err
	case "Extension":
		var ec ExtensionContent
		err = json.Unmarshal([]byte(input), &ec)

		return ec, err
	case "SF|Extension":
		var sfe SFExtensionContent

		if len(input) > 0 {
			err = json.Unmarshal([]byte(input), &sfe)
		}

		return sfe, err
	case "SF|MFA":
		var sfm SFMFAContent

		if len(input) > 0 {
			err = json.Unmarshal([]byte(input), &sfm)
		}

		return sfm, err
	case "SN|SmartTag":
		var st SmartTagContent

		if len(input) > 0 {
			err = json.Unmarshal([]byte(input), &st)
		}

		return st, err

	case "SN|FileSafe|FileMetadata":
		var fsfm FileSafeFileMetaDataContent

		if len(input) > 0 {
			err = json.Unmarshal([]byte(input), &fsfm)
		}

		return fsfm, err

	case "SN|FileSafe|Integration":
		var fsi FileSafeIntegrationContent

		if len(input) > 0 {
			err = json.Unmarshal([]byte(input), &fsi)
		}

		return fsi, err
	case "SN|UserPreferences":
		var upc UserPreferencesContent

		if len(input) > 0 {
			err = json.Unmarshal([]byte(input), &upc)
		}

		return upc, err
	case "SN|ExtensionRepo":
		var erc ExtensionRepoContent

		if len(input) > 0 {
			err = json.Unmarshal([]byte(input), &erc)
		}

		return erc, err
	case "SN|FileSafe|Credentials":
		var fsc FileSafeCredentialsContent

		if len(input) > 0 {
			err = json.Unmarshal([]byte(input), &fsc)
		}

		return fsc, err
	default:
		return nil, fmt.Errorf("unexpected type '%s'", contentType)
	}
}

func (ei *EncryptedItems) DeDupe() {
	if ei == nil {
		return
	}

	uniqueItems := make(map[string]EncryptedItem)

	var deDuped EncryptedItems

	eis := *ei
	for _, ei1 := range eis {
		if _, ok := uniqueItems[ei1.UUID]; ok {
			if ei1.UpdatedAtTimestamp > uniqueItems[ei1.UUID].UpdatedAtTimestamp {
				uniqueItems[ei1.UUID] = ei1
			}
		} else {
			uniqueItems[ei1.UUID] = ei1
		}
	}

	for _, v := range uniqueItems {
		deDuped = append(deDuped, v)
	}

	*ei = deDuped
}

func (ei *EncryptedItems) RemoveUnsupported() {
	var supported EncryptedItems

	for _, i := range *ei {
		if !stringInSlice(i.ContentType, []string{"SF|Extension"}, true) {
			supported = append(supported, i)
		}
	}

	*ei = supported
}

func (ei *EncryptedItems) RemoveDeleted() {
	var clean EncryptedItems

	for _, i := range *ei {
		if !i.Deleted {
			clean = append(clean, i)
		}
	}

	*ei = clean
}

func (i *Items) DeDupe() {
	var encountered []string

	var deDuped Items

	for _, j := range *i {
		if !stringInSlice(j.GetUUID(), encountered, true) {
			deDuped = append(deDuped, j)
		}

		encountered = append(encountered, j.GetUUID())
	}

	*i = deDuped
}

func (i *Items) RemoveDeleted() {
	var clean Items

	for _, j := range *i {
		if !j.IsDeleted() {
			clean = append(clean, j)
		}
	}

	*i = clean
}

func (di *DecryptedItems) RemoveDeleted() {
	var clean DecryptedItems

	for _, j := range *di {
		if !j.Deleted {
			clean = append(clean, j)
		}
	}

	*di = clean
}

func (s *Session) Export(path string) error {
	// we must export all items or otherwise we will update the encryption key for non exported items so they can no longer be encrypted
	so, err := Sync(SyncInput{
		Session: s,
	})
	if err != nil {
		return err
	}

	ik := s.DefaultItemsKey

	// create new items key, but copy across uuid and timestamps
	nk, err := s.CreateItemsKey()
	if err != nil {
		return err
	}

	nk.UUID = ik.UUID
	nk.CreatedAt = ik.CreatedAt
	nk.UpdatedAt = ik.UpdatedAt
	nk.CreatedAtTimestamp = ik.CreatedAtTimestamp
	nk.UpdatedAtTimestamp = ik.UpdatedAtTimestamp

	// encrypt items with the new ItemsKey
	nei, err := so.Items.ReEncrypt(s, ItemsKey{}, nk, s.MasterKey)
	if err != nil {
		return err
	}

	// encrypt items key that encrypted the items
	eik, err := nk.Encrypt(s, false)
	if err != nil {
		return err
	}

	if eik.UpdatedAtTimestamp != ik.UpdatedAtTimestamp {
		panic("updated timestamp not consistent with original")
	}

	eik.UpdatedAtTimestamp = ik.UpdatedAtTimestamp

	eik.UpdatedAt = ik.UpdatedAt

	// prepend new items key to the export
	nei = append([]EncryptedItem{eik}, nei...)

	// add existing items keys to the export
	if err = writeJSON(writeJSONConfig{
		session: *s,
		Path:    path,
		Debug:   true,
	}, nei); err != nil {
		return err
	}

	return nil
}

type EncryptedItemExport struct {
	UUID        string  `json:"uuid"`
	ItemsKeyID  *string `json:"items_key_id,omitempty"`
	Content     string  `json:"content"`
	ContentType string  `json:"content_type"`
	// Deleted            bool    `json:"deleted"`
	EncItemKey         string  `json:"enc_item_key"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	CreatedAtTimestamp int64   `json:"created_at_timestamp"`
	UpdatedAtTimestamp int64   `json:"updated_at_timestamp"`
	DuplicateOf        *string `json:"duplicate_of"`
}

type writeJSONConfig struct {
	session   Session
	plainText bool
	Path      string
	Debug     bool
}

func writeJSON(c writeJSONConfig, items EncryptedItems) error {
	// prepare for export
	var itemsExport []EncryptedItemExport

	for x := range items {
		itemsExport = append(itemsExport, EncryptedItemExport{
			UUID:       items[x].UUID,
			ItemsKeyID: items[x].ItemsKeyID,
			Content:    items[x].Content,
			// Deleted:            items[x].Deleted,
			ContentType:        items[x].ContentType,
			EncItemKey:         items[x].EncItemKey,
			CreatedAt:          items[x].CreatedAt,
			UpdatedAt:          items[x].UpdatedAt,
			CreatedAtTimestamp: items[x].CreatedAtTimestamp,
			UpdatedAtTimestamp: items[x].UpdatedAtTimestamp,
			DuplicateOf:        items[x].DuplicateOf,
		})
	}

	file, err := os.Create(c.Path)
	if err != nil {
		return err
	}

	defer file.Close()

	var jsonExport []byte
	if err == nil {
		jsonExport, err = json.MarshalIndent(itemsExport, "", "  ")
	}

	content := strings.Builder{}
	content.WriteString("{\n  \"version\": \"004\",")
	content.WriteString("\n  \"items\": ")
	content.WriteString(string(jsonExport))
	content.WriteString(",")

	// add keyParams
	content.WriteString("\n  \"keyParams\": {")
	content.WriteString(fmt.Sprintf("\n    \"identifier\": \"%s\",", c.session.KeyParams.Identifier))
	content.WriteString(fmt.Sprintf("\n    \"version\": \"%s\",", c.session.KeyParams.Version))
	content.WriteString(fmt.Sprintf("\n    \"origination\": \"%s\",", c.session.KeyParams.Origination))
	content.WriteString(fmt.Sprintf("\n    \"created\": \"%s\",", c.session.KeyParams.Created))
	content.WriteString(fmt.Sprintf("\n    \"pw_nonce\": \"%s\"", c.session.KeyParams.PwNonce))
	content.WriteString("\n  }")

	content.WriteString("\n}")
	_, err = file.WriteString(content.String())

	return err
}

type CompareEncryptedItemsInput struct {
	Session        *Session
	FirstItem      EncryptedItem
	FirstItemsKey  ItemsKey
	SecondItem     EncryptedItem
	SecondItemsKey ItemsKey
}

func compareEncryptedItems(input CompareEncryptedItemsInput) (same, unsupported bool, err error) {
	if input.FirstItem.ContentType != input.SecondItem.ContentType {
		return false, unsupported, nil
	}
	fDec, err := EncryptedItems{input.FirstItem}.Decrypt(input.Session, input.FirstItemsKey)
	if err != nil {
		return
	}
	fPar, err := fDec.Parse()
	if err != nil {
		return
	}
	sDec, err := EncryptedItems{input.SecondItem}.Decrypt(input.Session, input.SecondItemsKey)
	if err != nil {
		return
	}
	sPar, err := sDec.Parse()
	if err != nil {
		return
	}

	first := fPar[0]
	second := sPar[0]

	switch first.GetContentType() {
	case "Note":
		n1 := first.(*Note)
		n2 := second.(*Note)

		return n1.Content.Title == n2.Content.Title && n1.Content.Text == n2.Content.Text, unsupported, nil
	case "Tag":
		t1 := first.(*Tag)
		t2 := second.(*Tag)
		return t1.Content.Title == t2.Content.Title, unsupported, nil
	}

	return false, true, nil
}

// Import steps are:
// - decrypt items in current file (derive master key based on username, password nonce)
// - create a new items key and reencrypt all items
// - set items key to be same updatedtimestamp in order to replace existing
func (s *Session) Import(path string, syncToken string) (items EncryptedItems, itemsKey ItemsKey, err error) {
	initialItemsKey := fmt.Sprintf("Initial DefaultItemsKey %s", s.DefaultItemsKey.ItemsKey)

	encItemsToImport, keyParams, err := readJSON(path)
	if err != nil {
		return
	}

	// set master key to session by default, but then check if new one is required
	mk := s.MasterKey

	// if export was for a different user (identifier used to generate salt)
	// TODO: or nonce differs from session's
	if keyParams.Identifier != s.KeyParams.Identifier {
		debugPrint(s.Debug, "Import | export is from different account, so prompting for password")

		var password string

		fmt.Print("password: ")

		var bytePassword []byte
		bytePassword, err = terminal.ReadPassword(int(syscall.Stdin))

		fmt.Println()

		if err == nil {
			password = string(bytePassword)
		} else {
			return
		}

		if strings.TrimSpace(password) == "" {
			err = fmt.Errorf("password not defined")
			return
		}

		mk, _, err = generateMasterKeyAndServerPassword004(generateEncryptedPasswordInput{
			userPassword: password,
			authParamsOutput: authParamsOutput{
				Identifier:    keyParams.Identifier,
				PasswordNonce: keyParams.PwNonce,
				Version:       keyParams.Version,
			},
			debug: s.Debug,
		})
		if err != nil {
			return
		}
	}

	// retrieve items and itemskey from export
	var exportsEncItemsKeys EncryptedItems

	var exportedEncItems EncryptedItems

	for x := range encItemsToImport {
		if encItemsToImport[x].ContentType == "SN|ItemsKey" {
			exportsEncItemsKeys = append(exportsEncItemsKeys, encItemsToImport[x])

			continue
		}

		exportedEncItems = append(exportedEncItems, encItemsToImport[x])
	}

	// re-encrypt items
	if len(exportedEncItems) == 0 {
		err = fmt.Errorf("no items were found in export")

		return
	}

	var exportsItemsKey ItemsKey

	switch len(exportsEncItemsKeys) {
	case 0:
		err = fmt.Errorf("invalid export: missing ItemsKey %w", err)
		return
	case 1:
		var exportsItemsKeys ItemsKeys

		exportsItemsKeys, err = exportsEncItemsKeys.DecryptAndParseItemsKeys(mk, false)
		if err != nil {
			err = fmt.Errorf("invalid export: failed to decrypt ItemsKey %w", err)
			return
		}

		exportsItemsKey = exportsItemsKeys[0]
	default:
		err = fmt.Errorf("invalid export: only one ItemsKey expected but %d found", len(exportsEncItemsKeys))
		return
	}

	// re-encrypt all existing items with new key
	s.ImporterItemsKey = exportsItemsKey
	so, err := Sync(SyncInput{
		Session: s,
		// retrieve all items
		SyncToken: "",
	})

	if err != nil {
		return
	}

	// sync will override the default items key with the initial one found
	existingEncrypted := so.Items

	// determine whether existing or exported items should be resynced...
	// - if export and existing have same last updated time, then just choose exported version (already re-encrypted)
	var exportedToReencrypt EncryptedItems

	var existingToReencrypt EncryptedItems

	for x := range existingEncrypted {
		var match bool

		for y := range exportedEncItems {
			// check if we have a match for existing item and exported item
			if existingEncrypted[x].UUID == exportedEncItems[y].UUID && exportedEncItems[y].ContentType != "SN|ItemsKey" {
				match = true

				if existingEncrypted[x].UpdatedAtTimestamp > exportedEncItems[y].UpdatedAtTimestamp {
					debugPrint(s.Debug, "Import | existing %s %s newer than item to encrypt")
					// if existing item is newer, then re-encrypt existing and add to list
					existingToReencrypt = append(existingToReencrypt, existingEncrypted[x])

					var identical, unsupported bool
					// if exported item's content differs, then add also, and deal with conflict during sync
					identical, unsupported, err = compareEncryptedItems(CompareEncryptedItemsInput{
						Session:        s,
						FirstItem:      existingEncrypted[x],
						FirstItemsKey:  s.DefaultItemsKey,
						SecondItem:     exportedEncItems[y],
						SecondItemsKey: exportsItemsKey,
					})
					if err != nil {
						return
					}

					// if we're able to compare items, and they differ, then we'll add this item to intentionally
					// conflict on sync and be created as a conflicted copy
					if !identical && !unsupported {
						exportedToReencrypt = append(exportedToReencrypt, exportedEncItems[y])
					}
				} else if existingEncrypted[x].UpdatedAtTimestamp == exportedEncItems[y].UpdatedAtTimestamp {
					// if existing item same age, then choose exported version that's already encrypted with new key
					exportedToReencrypt = append(exportedToReencrypt, exportedEncItems[y])
				} else {
					// (exported cannot be newer than existing item)
					panic(fmt.Sprintf("exported %s %s found to be newer than server version",
						existingEncrypted[x].ContentType,
						existingEncrypted[x].UUID))
				}
			}
		}

		// if we didn't find a match for the item in the export (and it's not a key) then add to final list
		if !match && existingEncrypted[x].ContentType != "SN|ItemsKey" {
			existingToReencrypt = append(existingToReencrypt, existingEncrypted[x])
		}
	}

	// loop through items to import and import any non Items Key (already handled) that doesn't exist in cache
	for y := range exportedEncItems {
		var found bool

		for x := range existingEncrypted {
			if exportedEncItems[y].UUID == existingEncrypted[x].UUID {
				found = true

				break
			}
		}

		if !found {
			exportedToReencrypt = append(exportedToReencrypt, exportedEncItems[y])
		}
	}

	// create new items key and encrypt using current session's master key
	nik := NewItemsKey()
	nik.UUID = exportsEncItemsKeys[0].UUID
	nik.UpdatedAtTimestamp = s.DefaultItemsKey.UpdatedAtTimestamp
	nik.UpdatedAt = s.DefaultItemsKey.UpdatedAt

	rEncFinal, err := exportedToReencrypt.ReEncrypt(s, exportsItemsKey, nik, s.MasterKey)
	if err != nil {
		return
	}

	expEncFinal, err := existingToReencrypt.ReEncrypt(s, s.DefaultItemsKey, nik, s.MasterKey)
	if err != nil {
		return
	}

	// add items key used to encrypt items to final list to import
	if len(exportsEncItemsKeys) > 1 {
		panic("received more than one items key")
	}

	eNik, err := nik.Encrypt(s, false)
	if err != nil {
		return
	}

	eNiks := EncryptedItems{
		eNik,
	}

	rEncFinal = append(rEncFinal, expEncFinal...)
	rEncFinal = append(eNiks, rEncFinal...)

	s.DefaultItemsKey = nik
	s.ItemsKeys = ItemsKeys{s.DefaultItemsKey}

	so2, err := Sync(SyncInput{
		Session:   s,
		SyncToken: so.SyncToken,
		Items:     rEncFinal,
	})

	if err != nil {
		return
	}

	for x := range so.SavedItems {
		if so.SavedItems[x].ContentType == "SN|ItemsKey" {
			itemsKey, err = so.SavedItems[x].Decrypt(s.MasterKey)
			if err != nil {
				return
			}
		}
	}

	if initialItemsKey == exportsItemsKey.ItemsKey {
		panic("expected keys to differ")
	}

	items = so2.SavedItems
	itemsKey = nik

	return
}

func readJSON(filePath string) (items EncryptedItems, kp KeyParams, err error) {
	file, err := ioutil.ReadFile(filePath)
	if err != nil {
		err = fmt.Errorf("%w failed to open: %s", err, filePath)
		return
	}

	var eif EncryptedItemsFile

	err = json.Unmarshal(file, &eif)
	if err != nil {
		err = fmt.Errorf("failed to unmarshall json: %w", err)
		return
	}

	return eif.Items, eif.KeyParams, err
}

type EncryptedItemsFile struct {
	Items     EncryptedItems `json:"items"`
	KeyParams KeyParams      `json:"keyParams"`
}
