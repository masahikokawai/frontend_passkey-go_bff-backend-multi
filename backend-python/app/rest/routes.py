from __future__ import annotations

import json
import logging

from fastapi import APIRouter, Request, Response
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ValidationError

from app.auth.user_resolver import UserResolver
from app.domain.errors import TaskError
from app.domain.models import TaskInput
from app.domain.validation import parse_finished_on, validate_task_input
from app.repository.task_repository import TaskRepository
from app.rest.error_mapper import status_and_body
from app.rest.task_json import task_to_json

import datetime

log = logging.getLogger(__name__)


class TaskRequestBody(BaseModel):
    """Pydanticは構造的な型変換(JSON -> 型付きオブジェクト)にのみ使う。
    ビジネスルール(必須チェック・文字数制限・日付の過去日判定・statusのenum判定)は
    app.domain.validationの明示的な関数が担う(このプロジェクト全言語共通の方針、
    README.md「アーキテクチャ選定」節参照)。全フィールドをOptionalにしているのは、
    「必須フィールドが無い/空文字である」の判定自体をPydanticのバリデータではなく
    手書きロジックに委ねるため(backend-kotlinのparseBody関数と同じ設計)
    """

    name: str | None = None
    description: str | None = None
    status: str | None = None
    finished_on: str | None = None
    label_ids: list[int] = []


def create_router(repository: TaskRepository, user_resolver: UserResolver) -> APIRouter:
    router = APIRouter()

    async def authenticate(request: Request) -> int:
        return await user_resolver.resolve(request.headers.get("Authorization"))

    def parse_id(raw: str) -> int:
        try:
            return int(raw)
        except ValueError as exc:
            raise TaskError.invalid_id() from exc

    async def parse_body(request: Request) -> TaskInput:
        """backend(Go)のtaskRequestBody(binding:"required")と同じ「空文字も必須違反」の扱い"""
        try:
            raw = await request.json()
        except json.JSONDecodeError as exc:
            raise TaskError.invalid_request() from exc

        try:
            body = TaskRequestBody.model_validate(raw)
        except ValidationError as exc:
            raise TaskError.invalid_request() from exc

        if not body.name or not body.status or not body.finished_on:
            raise TaskError.invalid_request()

        finished_on = parse_finished_on(body.finished_on)
        return TaskInput(
            name=body.name,
            description=body.description,
            status_raw=body.status,
            finished_on=finished_on,
            label_ids=body.label_ids,
        )

    @router.get("/internal/v1/tasks")
    async def list_tasks(request: Request):
        user_id = await authenticate(request)
        limit = int(request.query_params.get("limit", "20"))
        offset = int(request.query_params.get("offset", "0"))
        log.debug("list_tasks user_id=%d limit=%d offset=%d", user_id, limit, offset)

        page = await repository.list_offset(user_id, limit, offset)
        return JSONResponse(
            {
                "tasks": [task_to_json(t) for t in page.tasks],
                "total": page.total,
                "limit": limit,
                "offset": offset,
            }
        )

    @router.get("/internal/v1/tasks/{id}")
    async def get_task(id: str, request: Request):
        user_id = await authenticate(request)
        task_id = parse_id(id)
        task = await repository.find_by_id(task_id, user_id)
        if task is None:
            raise TaskError.not_found()
        return JSONResponse(task_to_json(task))

    @router.post("/internal/v1/tasks")
    async def create_task(request: Request):
        user_id = await authenticate(request)
        input_ = await parse_body(request)
        today = datetime.datetime.now(datetime.timezone.utc).date()
        status = validate_task_input(input_, today)

        task_id = await repository.create(user_id, input_, status)
        task = await repository.find_by_id(task_id, user_id)
        if task is None:
            raise TaskError.internal("task disappeared after create")
        return JSONResponse(task_to_json(task), status_code=201)

    @router.patch("/internal/v1/tasks/{id}")
    async def update_task(id: str, request: Request):
        user_id = await authenticate(request)
        task_id = parse_id(id)
        input_ = await parse_body(request)
        today = datetime.datetime.now(datetime.timezone.utc).date()
        status = validate_task_input(input_, today)

        updated = await repository.update(task_id, user_id, input_, status)
        if not updated:
            raise TaskError.not_found()
        task = await repository.find_by_id(task_id, user_id)
        if task is None:
            raise TaskError.not_found()
        return JSONResponse(task_to_json(task))

    @router.delete("/internal/v1/tasks/{id}")
    async def delete_task(id: str, request: Request):
        user_id = await authenticate(request)
        task_id = parse_id(id)
        deleted = await repository.delete(task_id, user_id)
        if not deleted:
            raise TaskError.not_found()
        return Response(status_code=204)

    return router


def register_error_handler(app) -> None:
    @app.exception_handler(TaskError)
    async def handle_task_error(request: Request, exc: TaskError) -> JSONResponse:
        result = status_and_body(exc)
        return JSONResponse(result.body, status_code=result.status)

    @app.exception_handler(Exception)
    async def handle_unexpected_error(request: Request, exc: Exception) -> JSONResponse:
        result = status_and_body(TaskError.internal(str(exc)))
        return JSONResponse(result.body, status_code=result.status)
