---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Review a candidate finding
description: Validate a candidate against a concrete trigger and supporting evidence.
docType: how-to
---

Use this procedure to distinguish a model's candidate finding from a validated conclusion.

## Prerequisites

Identify the affected source and the scope of the review. The template is `web/docs/.qa/template-ai-review.md`. Focused working reviews exist in `docs/reviews/`; check their scope and date before treating a conclusion as current.

## Procedure

1. Identify the relevant source and the concrete input or state that triggers the candidate issue.
2. Trace the code path or run a suitable test to establish the observed consequence. Review the owning schema or shared contract when generated code is involved.
3. Record whether the candidate is confirmed, rejected or unresolved, and explain the evidence and reasoning.
4. If fixed, link the change and the validation that covers the trigger.

## Verify and record

Check that another reviewer can follow the trigger, source and evidence to the stated conclusion. A confident paragraph alone is not evidence. Follow the [validation evidence](/qa/) guidance and use [testing](/guides/testing/) for test commands.

## Limitations

Keep model-generated candidates separate from human validation. No numbered AI-review reports are published in this section.
