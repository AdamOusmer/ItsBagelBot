// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package assets

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"ItsBagelBot/app/db/overlays/importer"
	"ItsBagelBot/app/db/overlays/media"
	"ItsBagelBot/app/db/overlays/repository"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/objectstore"
)

type upload struct {
	Ticket Ticket
	Body   io.Reader
	Size   int64
	DryRun bool
}

type uploadAction func(ctx context.Context, up upload) (any, error)

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT "+overlaysrpc.AssetUploadPath+"{ticket}", s.handle(PurposeAsset, s.uploadAsset))
	mux.HandleFunc("PUT "+overlaysrpc.ImportUploadPath+"{ticket}", s.handle(PurposeImport, s.uploadImport))
	return mux
}

func (s *Service) handle(purpose Purpose, finish uploadAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ticket, err := s.authorize(r, purpose)
		if err != nil {
			writeError(w, err)
			return
		}
		var out any
		if r.URL.Query().Get(overlaysrpc.DoneQuery) == "1" {
			out, err = s.complete(r, ticket, finish)
		} else {
			out, err = s.receivePart(r, ticket)
		}
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Service) authorize(r *http.Request, purpose Purpose) (Ticket, error) {
	ticket, err := s.Signer.Verify(r.PathValue("ticket"), s.now())
	switch {
	case err != nil:
		return Ticket{}, err
	case ticket.Purpose != purpose:
		return Ticket{}, ErrBadTicket
	case r.ContentLength > s.partSize():
		return Ticket{}, &http.MaxBytesError{Limit: s.partSize()}
	}
	return ticket, nil
}

func (s *Service) partNumber(r *http.Request, t Ticket) (int, error) {
	number, err := strconv.Atoi(r.URL.Query().Get(overlaysrpc.PartQuery))
	if err != nil {
		return 0, ErrInvalidPart
	}
	if number < 1 || number > s.parts(t) {
		return 0, ErrInvalidPart
	}
	if r.ContentLength <= 0 {
		return 0, ErrInvalidSize
	}
	return number, nil
}

func (s *Service) receivePart(r *http.Request, t Ticket) (any, error) {
	number, err := s.partNumber(r, t)
	if err != nil {
		return nil, err
	}
	object := objectstore.Object{
		Key: partKey(r.PathValue("ticket"), number), Body: r.Body,
		Size: r.ContentLength, ContentType: "application/octet-stream",
	}
	return map[string]int{"part": number}, s.Objects.Put(r.Context(), object)
}

func (s *Service) complete(r *http.Request, t Ticket, finish uploadAction) (any, error) {
	token := r.PathValue("ticket")
	defer func() { _ = s.Objects.DeletePrefix(context.WithoutCancel(r.Context()), partPrefix(token)) }()
	stream, size, err := s.openParts(r.Context(), token, s.parts(t))
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	if size > t.MaxSize {
		return nil, &http.MaxBytesError{Limit: t.MaxSize}
	}
	dry := t.DryRun || r.URL.Query().Get(overlaysrpc.DryRunQuery) == "1"
	return finish(r.Context(), upload{Ticket: t, Body: stream, Size: size, DryRun: dry})
}

func (s *Service) uploadAsset(ctx context.Context, up upload) (any, error) {
	result, data, err := media.Normalize(ctx, up.Body, up.Size)
	if err != nil {
		return nil, err
	}
	asset, err := s.Store(ctx, up.Ticket.UserID, Blob{Name: up.Ticket.Name, Result: result, Data: data})
	if err != nil {
		return nil, err
	}
	return s.View(asset), nil
}

type refusal struct {
	err    error
	status int
	code   string
}

var refusals = []refusal{
	{ErrBadTicket, http.StatusUnauthorized, "invalid_ticket"},
	{ErrQuota, http.StatusForbidden, "quota_exceeded"},
	{ErrInvalidPart, http.StatusBadRequest, "invalid_part"},
	{ErrInvalidSize, http.StatusBadRequest, "invalid_size"},
	{ErrIncomplete, http.StatusConflict, "incomplete_upload"},
	{repository.ErrOverlayLimit, http.StatusForbidden, "overlay_limit"},
	{repository.ErrInvalidDocument, http.StatusUnprocessableEntity, "invalid_document"},
	{repository.ErrForeignAsset, http.StatusUnprocessableEntity, "unknown_asset"},
}

func classify(err error) (int, string) {
	for _, known := range refusals {
		if errors.Is(err, known.err) {
			return known.status, known.code
		}
	}
	return classifyTyped(err)
}

func classifyTyped(err error) (int, string) {
	var tooBig *http.MaxBytesError
	var archive *importer.Error
	switch code := media.CodeOf(err); {
	case errors.As(err, &tooBig), code == media.CodeTooLarge:
		return http.StatusRequestEntityTooLarge, "too_large"
	case code != "":
		return http.StatusUnprocessableEntity, string(code)
	case errors.As(err, &archive):
		return http.StatusUnprocessableEntity, string(archive.Code)
	}
	return http.StatusInternalServerError, "internal"
}

func writeError(w http.ResponseWriter, err error) {
	status, code := classify(err)
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = codec.NewEncoder(w).Encode(body)
}
