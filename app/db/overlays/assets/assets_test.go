// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package assets_test

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/overlays/assets"
	"ItsBagelBot/app/db/overlays/ent"
	"ItsBagelBot/app/db/overlays/ent/enttest"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/overlays"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/objectstore/objectstoretest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

const (
	premiumUser = 42
	freeUser    = 43
)

type plans map[uint64]bool

func (p plans) Alpha(_ context.Context, userID uint64) (bool, error) { return p[userID], nil }

type fixture struct {
	svc     *assets.Service
	repo    *repository.Overlays
	objects *objectstoretest.Memory
	clock   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	f := &fixture{objects: objectstoretest.NewMemory(), clock: time.Unix(1_800_000_000, 0)}
	f.repo = repository.New(client, bustest.NewPublisher(), f.objects, "https://cdn.itsbagelbot.com")
	f.svc = &assets.Service{
		Repo: f.repo, Objects: f.objects, Alphas: plans{premiumUser: true},
		Signer: assets.Signer{Secret: []byte("secret")}, PublicURL: "https://cdn.itsbagelbot.com/",
		Now: func() time.Time { return f.clock },
	}
	return f
}

func pngOf(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, 0, color.RGBA{R: uint8(x * 40), A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func (f *fixture) assetTicket(t *testing.T, user uint64, size int) string {
	t.Helper()
	ticket, err := f.svc.BeginAsset(t.Context(), user, overlaysrpc.AssetBeginRequest{DisplayName: "logo.png", DeclaredSize: int64(size)})
	require.NoError(t, err)
	return ticket.Ticket
}

func (f *fixture) put(path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	f.svc.Handler().ServeHTTP(rec, req)
	return rec
}

func (f *fixture) upload(path string, body []byte, doneQuery string) *httptest.ResponseRecorder {
	size := int(f.svc.PartSize)
	if size == 0 {
		size = len(body)
	}
	number := 0
	for start := 0; start < len(body); start += size {
		number++
		part := f.put(path+"?part="+strconv.Itoa(number), body[start:min(start+size, len(body))])
		if part.Code != http.StatusOK {
			return part
		}
	}
	return f.put(path+"?done=1"+doneQuery, nil)
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	require.NoError(t, codec.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func TestUploadStoresUnderARandomKeyAndDedupes(t *testing.T) {
	f := newFixture(t)
	body := pngOf(4, 4)

	first := f.upload(overlaysrpc.AssetUploadPath+f.assetTicket(t, premiumUser, len(body)), body, "")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	view := decode[overlaysrpc.AssetView](t, first)
	again := decode[overlaysrpc.AssetView](t, f.upload(overlaysrpc.AssetUploadPath+f.assetTicket(t, premiumUser, len(body)), body, ""))

	key := "u/42/" + view.ID
	info, err := f.objects.Head(t.Context(), key)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"keys": []string{key}, "url": "https://cdn.itsbagelbot.com/" + key, "same": view.ID, "type": "image/png",
		"cache": assets.CacheControl, "dims": [2]int{4, 4},
	}, map[string]any{
		"keys": f.objects.Keys(), "url": view.URL, "same": again.ID, "type": info.ContentType,
		"cache": info.CacheControl, "dims": [2]int{view.W, view.H},
	})
	require.Len(t, view.ID, 32)
}

func TestBeginRefuses(t *testing.T) {
	f := newFixture(t)
	_, err := f.repo.StoreAsset(t.Context(), repository.Asset{
		ID: "seed", UserID: premiumUser, Type: overlays.AssetPNG, Bytes: overlays.AccountMaxBytes, SHA256: strings.Repeat("b", 64),
	})
	require.NoError(t, err)

	cases := []struct {
		name string
		svc  *assets.Service
		user uint64
		size int64
		want error
	}{
		{"plain account", f.svc, freeUser, 10, assets.ErrAlphaRequired},
		{"account storage full", f.svc, premiumUser, 10, assets.ErrQuota},
		{"no size", f.svc, premiumUser, 0, assets.ErrInvalidSize},
		{"larger than any asset", f.svc, premiumUser, 1 << 40, assets.ErrInvalidSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.svc.BeginAsset(t.Context(), tc.user, overlaysrpc.AssetBeginRequest{DeclaredSize: tc.size})
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestUploadRefusals(t *testing.T) {
	f := newFixture(t)
	body := pngOf(4, 4)
	valid := f.assetTicket(t, premiumUser, len(body))
	small := f.assetTicket(t, premiumUser, 8)
	importTicket, err := f.svc.BeginImport(t.Context(), premiumUser, overlaysrpc.ImportBeginRequest{DeclaredSize: 100})
	require.NoError(t, err)
	expired := f.assetTicket(t, premiumUser, len(body))

	cases := []struct {
		name   string
		ticket string
		body   []byte
		age    time.Duration
		want   outcome
	}{
		{"garbage ticket", "garbage", body, 0, outcome{http.StatusUnauthorized, "invalid_ticket"}},
		{"tampered ticket", valid[:len(valid)-2] + "AA", body, 0, outcome{http.StatusUnauthorized, "invalid_ticket"}},
		{"expired ticket", expired, body, assets.TicketTTL + time.Second, outcome{http.StatusUnauthorized, "invalid_ticket"}},
		{"import ticket on the asset path", importTicket.Ticket, body, 0, outcome{http.StatusUnauthorized, "invalid_ticket"}},
		{"body larger than the ticket", small, body, 0, outcome{http.StatusRequestEntityTooLarge, "too_large"}},
		{"not media", valid, []byte("hello world, not an image"), 0, outcome{http.StatusUnprocessableEntity, "unsupported_media"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.clock = time.Unix(1_800_000_000, 0).Add(tc.age)
			rec := f.upload(overlaysrpc.AssetUploadPath+tc.ticket, tc.body, "")
			got := decode[map[string]string](t, rec)
			require.Equal(t, tc.want, outcome{rec.Code, got["error"]})
		})
	}
	require.Empty(t, f.objects.Keys())
}

type outcome struct {
	status int
	code   string
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func exportZip(t *testing.T) []byte {
	export := `{"name":"Mine","width":1920,"height":1080,"widgets":[
		{"type":"Alert Box","name":"Alerts","x":10,"y":20,"width":500,"height":300,"settings":{"image":"logo.png","delay":3}}]}`
	return zipOf(t, map[string][]byte{"logo.png": pngOf(4, 4), "overlay.json": []byte(export)})
}

func (f *fixture) importTicket(t *testing.T, user uint64, size int, dryRun bool) string {
	t.Helper()
	ticket, err := f.svc.BeginImport(t.Context(), user, overlaysrpc.ImportBeginRequest{DeclaredSize: int64(size), DryRun: dryRun})
	require.NoError(t, err)
	return ticket.Ticket
}

func TestImportDryRunStoresNothing(t *testing.T) {
	f := newFixture(t)
	archive := exportZip(t)
	cases := []struct {
		name string
		user uint64
		path string
		done string
	}{
		{"alpha account with a preview ticket", premiumUser, overlaysrpc.ImportUploadPath + f.importTicket(t, premiumUser, len(archive), true), ""},
		{"alpha account asking for a preview", premiumUser, overlaysrpc.ImportUploadPath + f.importTicket(t, premiumUser, len(archive), false), "&dry_run=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.upload(tc.path, archive, tc.done)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			result := decode[overlaysrpc.ImportResult](t, rec)
			list, err := f.repo.ListOverlays(t.Context(), tc.user)
			require.NoError(t, err)
			require.Equal(t, [4]int{2, 0, 0, 0}, [4]int{len(result.Report.Mapped), len(list), len(f.objects.Keys()), int(result.OverlayID)})
		})
	}
}

func TestImportCreatesACustomOverlay(t *testing.T) {
	f := newFixture(t)
	archive := exportZip(t)

	rec := f.upload(overlaysrpc.ImportUploadPath+f.importTicket(t, premiumUser, len(archive), false), archive, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	result := decode[overlaysrpc.ImportResult](t, rec)
	overlay, err := f.repo.GetOverlay(t.Context(), premiumUser, result.OverlayID)
	require.NoError(t, err)
	doc, err := overlays.Parse(overlay.Document)
	require.NoError(t, err)
	stored, err := f.repo.ListAssets(t.Context(), premiumUser)
	require.NoError(t, err)

	require.Len(t, stored, 1)
	require.Equal(t, map[string]any{
		"kind": "custom", "name": "Mine", "ref": "asset:" + string(stored[0].ID), "assets": []string{string(stored[0].ID)},
		"keys": []string{stored[0].Key()}, "url": true,
	}, map[string]any{
		"kind": overlay.Kind, "name": overlay.Name, "ref": doc.Widgets[0].Settings["image"], "assets": []string{firstKey(doc.Assets)},
		"keys": f.objects.Keys(), "url": strings.HasPrefix(result.URL, overlaysrpc.OverlayURLPrefix),
	})
}

func firstKey(m map[string]overlays.Asset) string {
	for key := range m {
		return key
	}
	return ""
}

func TestBeginRefusesAfterAHourOfUploads(t *testing.T) {
	f := newFixture(t)
	f.clock = time.Now()
	for i := range overlays.AccountUploadsPerHour {
		_, err := f.repo.StoreAsset(t.Context(), repository.Asset{
			ID: repository.AssetID(strconv.Itoa(i)), UserID: premiumUser, Type: overlays.AssetPNG, Bytes: 1, SHA256: strings.Repeat(strconv.Itoa(i%10), 63) + strconv.Itoa(i/10),
		})
		require.NoError(t, err)
	}

	_, err := f.svc.BeginAsset(t.Context(), premiumUser, overlaysrpc.AssetBeginRequest{DeclaredSize: 10})
	f.clock = f.clock.Add(time.Hour + time.Minute)
	_, later := f.svc.BeginAsset(t.Context(), premiumUser, overlaysrpc.AssetBeginRequest{DeclaredSize: 10})

	require.ErrorIs(t, err, assets.ErrRateLimited)
	require.NoError(t, later)
}

func TestUploadInPartsAssemblesAndCleansUp(t *testing.T) {
	f := newFixture(t)
	f.svc.PartSize = 40
	body := pngOf(4, 4)
	ticket, err := f.svc.BeginAsset(t.Context(), premiumUser, overlaysrpc.AssetBeginRequest{DeclaredSize: int64(len(body))})
	require.NoError(t, err)

	rec := f.upload(overlaysrpc.AssetUploadPath+ticket.Ticket, body, "")
	missing := f.put(overlaysrpc.AssetUploadPath+ticket.Ticket+"?done=1", nil)

	view := decode[overlaysrpc.AssetView](t, rec)
	require.Equal(t, map[string]any{"parts": (len(body) + 39) / 40, "keys": []string{"u/42/" + view.ID}, "again": http.StatusConflict},
		map[string]any{"parts": ticket.Parts, "keys": f.objects.Keys(), "again": missing.Code})
}

func TestTicketsStopWorkingWhenAlphaIsRevoked(t *testing.T) {
	f := newFixture(t)
	archive := exportZip(t)
	cases := []struct {
		name string
		path string
	}{
		{"asset ticket", overlaysrpc.AssetUploadPath + f.assetTicket(t, premiumUser, len(pngOf(4, 4)))},
		{"import ticket", overlaysrpc.ImportUploadPath + f.importTicket(t, premiumUser, len(archive), false)},
	}
	f.svc.Alphas.(plans)[premiumUser] = false
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.put(tc.path+"?part=1", pngOf(4, 4))
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			require.Empty(t, f.objects.Keys())
		})
	}
}
