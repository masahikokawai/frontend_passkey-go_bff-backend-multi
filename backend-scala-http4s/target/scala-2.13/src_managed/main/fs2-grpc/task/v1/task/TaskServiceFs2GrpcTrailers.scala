package task.v1.task

import _root_.cats.syntax.all._

trait TaskServiceFs2GrpcTrailers[F[_], A] {
  def listTasks(request: task.v1.task.ListTasksRequest, ctx: A): F[(task.v1.task.ListTasksResponse, _root_.io.grpc.Metadata)]
  def getTask(request: task.v1.task.GetTaskRequest, ctx: A): F[(task.v1.task.Task, _root_.io.grpc.Metadata)]
  def createTask(request: task.v1.task.CreateTaskRequest, ctx: A): F[(task.v1.task.Task, _root_.io.grpc.Metadata)]
  def updateTask(request: task.v1.task.UpdateTaskRequest, ctx: A): F[(task.v1.task.Task, _root_.io.grpc.Metadata)]
  def deleteTask(request: task.v1.task.DeleteTaskRequest, ctx: A): F[(task.v1.task.DeleteTaskResponse, _root_.io.grpc.Metadata)]
}

object TaskServiceFs2GrpcTrailers extends _root_.fs2.grpc.GeneratedCompanion[TaskServiceFs2GrpcTrailers] {
  
  def serviceDescriptor: _root_.io.grpc.ServiceDescriptor = task.v1.task.TaskServiceGrpc.SERVICE
  
  def mkClient[F[_]: _root_.cats.effect.Async, A](dispatcher: _root_.cats.effect.std.Dispatcher[F], channel: _root_.io.grpc.Channel, mkMetadata: A => F[_root_.io.grpc.Metadata], clientOptions: _root_.fs2.grpc.client.ClientOptions): TaskServiceFs2GrpcTrailers[F, A] = new TaskServiceFs2GrpcTrailers[F, A] {
    def listTasks(request: task.v1.task.ListTasksRequest, ctx: A): F[(task.v1.task.ListTasksResponse, _root_.io.grpc.Metadata)] = {
      mkMetadata(ctx).flatMap { m =>
        _root_.fs2.grpc.client.Fs2ClientCall[F](channel, task.v1.task.TaskServiceGrpc.METHOD_LIST_TASKS, dispatcher, clientOptions).flatMap(_.unaryToUnaryCallTrailers(request, m))
      }
    }
    def getTask(request: task.v1.task.GetTaskRequest, ctx: A): F[(task.v1.task.Task, _root_.io.grpc.Metadata)] = {
      mkMetadata(ctx).flatMap { m =>
        _root_.fs2.grpc.client.Fs2ClientCall[F](channel, task.v1.task.TaskServiceGrpc.METHOD_GET_TASK, dispatcher, clientOptions).flatMap(_.unaryToUnaryCallTrailers(request, m))
      }
    }
    def createTask(request: task.v1.task.CreateTaskRequest, ctx: A): F[(task.v1.task.Task, _root_.io.grpc.Metadata)] = {
      mkMetadata(ctx).flatMap { m =>
        _root_.fs2.grpc.client.Fs2ClientCall[F](channel, task.v1.task.TaskServiceGrpc.METHOD_CREATE_TASK, dispatcher, clientOptions).flatMap(_.unaryToUnaryCallTrailers(request, m))
      }
    }
    def updateTask(request: task.v1.task.UpdateTaskRequest, ctx: A): F[(task.v1.task.Task, _root_.io.grpc.Metadata)] = {
      mkMetadata(ctx).flatMap { m =>
        _root_.fs2.grpc.client.Fs2ClientCall[F](channel, task.v1.task.TaskServiceGrpc.METHOD_UPDATE_TASK, dispatcher, clientOptions).flatMap(_.unaryToUnaryCallTrailers(request, m))
      }
    }
    def deleteTask(request: task.v1.task.DeleteTaskRequest, ctx: A): F[(task.v1.task.DeleteTaskResponse, _root_.io.grpc.Metadata)] = {
      mkMetadata(ctx).flatMap { m =>
        _root_.fs2.grpc.client.Fs2ClientCall[F](channel, task.v1.task.TaskServiceGrpc.METHOD_DELETE_TASK, dispatcher, clientOptions).flatMap(_.unaryToUnaryCallTrailers(request, m))
      }
    }
  }
  
  protected def serviceBinding[F[_]: _root_.cats.effect.Async, A](dispatcher: _root_.cats.effect.std.Dispatcher[F], serviceImpl: TaskServiceFs2GrpcTrailers[F, A], mkCtx: _root_.io.grpc.Metadata => F[A], serverOptions: _root_.fs2.grpc.server.ServerOptions): _root_.io.grpc.ServerServiceDefinition = {
    _root_.io.grpc.ServerServiceDefinition
      .builder(task.v1.task.TaskServiceGrpc.SERVICE)
      .addMethod(task.v1.task.TaskServiceGrpc.METHOD_LIST_TASKS, _root_.fs2.grpc.server.Fs2ServerCallHandler[F](dispatcher, serverOptions).unaryToUnaryCallTrailers[task.v1.task.ListTasksRequest, task.v1.task.ListTasksResponse]((r, m) => mkCtx(m).flatMap(serviceImpl.listTasks(r, _))))
      .addMethod(task.v1.task.TaskServiceGrpc.METHOD_GET_TASK, _root_.fs2.grpc.server.Fs2ServerCallHandler[F](dispatcher, serverOptions).unaryToUnaryCallTrailers[task.v1.task.GetTaskRequest, task.v1.task.Task]((r, m) => mkCtx(m).flatMap(serviceImpl.getTask(r, _))))
      .addMethod(task.v1.task.TaskServiceGrpc.METHOD_CREATE_TASK, _root_.fs2.grpc.server.Fs2ServerCallHandler[F](dispatcher, serverOptions).unaryToUnaryCallTrailers[task.v1.task.CreateTaskRequest, task.v1.task.Task]((r, m) => mkCtx(m).flatMap(serviceImpl.createTask(r, _))))
      .addMethod(task.v1.task.TaskServiceGrpc.METHOD_UPDATE_TASK, _root_.fs2.grpc.server.Fs2ServerCallHandler[F](dispatcher, serverOptions).unaryToUnaryCallTrailers[task.v1.task.UpdateTaskRequest, task.v1.task.Task]((r, m) => mkCtx(m).flatMap(serviceImpl.updateTask(r, _))))
      .addMethod(task.v1.task.TaskServiceGrpc.METHOD_DELETE_TASK, _root_.fs2.grpc.server.Fs2ServerCallHandler[F](dispatcher, serverOptions).unaryToUnaryCallTrailers[task.v1.task.DeleteTaskRequest, task.v1.task.DeleteTaskResponse]((r, m) => mkCtx(m).flatMap(serviceImpl.deleteTask(r, _))))
      .build()
  }

}