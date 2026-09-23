from __future__ import annotations

import logging

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from app.auth.claims import VerifyException
from app.auth.dispatcher import Dispatcher
from app.auth.external_auth import require_external_client
from app.domain.errors import TaskError
from app.external.query import ExternalQueryError, parse_cursor_query, parse_offset_query, parse_user_id
from app.flags.feature_flag_poller import FeatureFlagPoller
from app.repository.task_repository import TaskRepository
from app.rest.task_json import task_to_json

PAGINATION_V2_FLAG_KEY = "backend.external-tasks-pagination-v2"

log = logging.getLogger(__name__)


def create_external_router(
    repository: TaskRepository,
    dispatcher: Dispatcher,
    flags: FeatureFlagPoller,
    external_api_client_id: str,
) -> APIRouter:
    router = APIRouter()

    @router.get("/external/v1/tasks")
    async def list_tasks_external(request: Request):
        try:
            await require_external_client(dispatcher, request.headers.get("Authorization"), external_api_client_id)
        except (TaskError, VerifyException):
            return JSONResponse({"error": "unauthenticated"}, status_code=401)

        try:
            user_id = parse_user_id(request.query_params.get("user_id"))
        except ExternalQueryError as exc:
            return JSONResponse({"error": exc.error_key}, status_code=400)

        use_v2 = flags.variation(PAGINATION_V2_FLAG_KEY, "off") == "on"
        log.debug("list_tasks_external user_id=%d pagination_v2=%s", user_id, use_v2)

        if use_v2:
            q = parse_cursor_query(request.query_params.get("cursor"), request.query_params.get("limit"))
            tasks = await repository.list_cursor(user_id, q.after_id, q.limit)
            next_cursor = str(tasks[-1].id) if tasks else None
            return JSONResponse(
                {
                    "tasks": [task_to_json(t) for t in tasks],
                    "next_cursor": next_cursor,
                    "limit": q.limit,
                }
            )

        q = parse_offset_query(request.query_params.get("page"), request.query_params.get("page_size"))
        offset = (q.page - 1) * q.page_size
        page = await repository.list_offset(user_id, q.page_size, offset)
        return JSONResponse(
            {
                "tasks": [task_to_json(t) for t in page.tasks],
                "page": q.page,
                "page_size": q.page_size,
                "total": page.total,
            }
        )

    return router
