resource "aws_cloudwatch_log_group" "ecs" {
  name              = "/ecs/${local.name}"
  retention_in_days = 1
}

resource "aws_ecs_cluster" "main" {
  name = local.name
}

data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "task_execution" {
  name               = "${local.name}-task-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy_attachment" "task_execution" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

locals {
  app_image      = "${aws_ecr_repository.app.repository_url}:${var.image_tag}"
  temporal_image = "${aws_ecr_repository.app.repository_url}:${var.temporal_image_tag}"

  common_env = {
    DATABASE_URL       = local.database_url
    TEMPORAL_ADDRESS   = "temporal.payout.local:7233"
    MOCKS_URL          = "http://mocks.payout.local:8090"
    RECEIVER_URL       = "http://receiver.payout.local:8095/events"
    API_TOKEN          = var.api_token
    OTEL_TRACES_STDOUT = "true"
    LOG_LEVEL          = "info"
  }

  # lb_port is the public ALB listener (0 = not exposed); container_port is where the ALB forwards.
  services = {
    temporal = {
      image = local.temporal_image, command = ["server", "start-dev", "--ip", "0.0.0.0", "--port", "7233", "--ui-port", "8233"]
      cpu   = 512, memory = 1024, env = {}, discover = true, lb_port = 8233, container_port = 8233, health = "/"
    }
    mocks = {
      image    = local.app_image, command = ["mocks"], cpu = 256, memory = 512, env = {}
      discover = true, lb_port = 8090, container_port = 8090, health = "/healthz"
    }
    receiver = {
      image    = local.app_image, command = ["receiver"], cpu = 256, memory = 512, env = {}
      discover = true, lb_port = 8095, container_port = 8095, health = "/healthz"
    }
    api = {
      image    = local.app_image, command = ["api"], cpu = 256, memory = 512, env = {}
      discover = true, lb_port = 8080, container_port = 8080, health = "/healthz"
    }
    worker = {
      image    = local.app_image, command = ["worker"], cpu = 256, memory = 512, env = {}
      discover = false, lb_port = 0, container_port = 9090, health = ""
    }
    relay = {
      image    = local.app_image, command = ["relay"], cpu = 256, memory = 512, env = {}
      discover = false, lb_port = 0, container_port = 8096, health = ""
    }
    ops-ui = {
      image = local.app_image, command = ["ops-ui"], cpu = 256, memory = 512
      env = {
        API_URL         = "http://api.payout.local:8080"
        TEMPORAL_UI_URL = "http://${aws_lb.main.dns_name}:8233"
        DEBUG_SQL       = "true" # read-only SQL endpoint for the cloud drills (RDS is private)
      }
      discover = false, lb_port = 80, container_port = 8081, health = "/healthz"
    }
  }

  lb_services        = { for k, v in local.services : k => v if v.lb_port != 0 }
  discovered_service = { for k, v in local.services : k => v if v.discover }
}

resource "aws_ecs_task_definition" "svc" {
  for_each                 = local.services
  family                   = "${local.name}-${each.key}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = each.value.cpu
  memory                   = each.value.memory
  execution_role_arn       = aws_iam_role.task_execution.arn

  container_definitions = jsonencode([{
    name         = each.key
    image        = each.value.image
    command      = each.value.command
    essential    = true
    portMappings = [{ containerPort = each.value.container_port, protocol = "tcp" }]
    environment  = [for k, v in merge(local.common_env, each.value.env) : { name = k, value = v }]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.ecs.name
        awslogs-region        = var.region
        awslogs-stream-prefix = each.key
      }
    }
  }])
}

# One-off task used by scripts/deploy.sh to run migrations and seed data (RDS is private).
resource "aws_ecs_task_definition" "migrate" {
  family                   = "${local.name}-migrate"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.task_execution.arn

  container_definitions = jsonencode([{
    name        = "migrate"
    image       = local.app_image
    command     = ["migrate", "up", "seed"]
    essential   = true
    environment = [{ name = "DATABASE_URL", value = local.database_url }]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.ecs.name
        awslogs-region        = var.region
        awslogs-stream-prefix = "migrate"
      }
    }
  }])
}

resource "aws_service_discovery_service" "svc" {
  for_each = local.discovered_service
  name     = each.key
  dns_config {
    namespace_id = aws_service_discovery_private_dns_namespace.main.id
    dns_records {
      ttl  = 10
      type = "A"
    }
    routing_policy = "MULTIVALUE"
  }
  health_check_custom_config {}
  force_destroy = true
}

resource "aws_ecs_service" "svc" {
  for_each        = local.services
  name            = each.key
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.svc[each.key].arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.public[*].id
    security_groups  = [aws_security_group.app.id]
    assign_public_ip = true # public subnets, no NAT: needed to pull from ECR
  }

  dynamic "load_balancer" {
    for_each = each.value.lb_port != 0 ? [1] : []
    content {
      target_group_arn = aws_lb_target_group.svc[each.key].arn
      container_name   = each.key
      container_port   = each.value.container_port
    }
  }

  dynamic "service_registries" {
    for_each = each.value.discover ? [1] : []
    content {
      registry_arn = aws_service_discovery_service.svc[each.key].arn
    }
  }

  depends_on = [aws_lb_listener.svc]
}
