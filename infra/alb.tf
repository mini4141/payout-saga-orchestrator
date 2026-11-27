resource "aws_lb" "main" {
  name               = local.name
  load_balancer_type = "application"
  internal           = false
  subnets            = aws_subnet.public[*].id
  security_groups    = [aws_security_group.alb.id]
}

resource "aws_lb_target_group" "svc" {
  for_each             = local.lb_services
  name                 = "${local.name}-${each.key}"
  port                 = each.value.container_port
  protocol             = "HTTP"
  target_type          = "ip"
  vpc_id               = aws_vpc.main.id
  deregistration_delay = 5

  health_check {
    path                = each.value.health
    matcher             = "200-399"
    interval            = 10
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 5
  }
}

# One listener per service: 80 ops UI, 8080 Connect API, 8090 mocks admin, 8095 receiver, 8233 Temporal UI.
resource "aws_lb_listener" "svc" {
  for_each          = local.lb_services
  load_balancer_arn = aws_lb.main.arn
  port              = each.value.lb_port
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.svc[each.key].arn
  }
}
