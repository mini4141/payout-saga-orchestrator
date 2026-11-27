output "alb_dns" {
  value = aws_lb.main.dns_name
}

output "ecr_repository_url" {
  value = aws_ecr_repository.app.repository_url
}

output "cluster_name" {
  value = aws_ecs_cluster.main.name
}

output "state_machine_arn" {
  value = aws_sfn_state_machine.payout.arn
}

output "recon_schedule" {
  value = aws_scheduler_schedule.recon.name
}

output "db_identifier" {
  value = aws_db_instance.main.identifier
}

output "subnet_ids" {
  value = aws_subnet.public[*].id
}

output "app_security_group_id" {
  value = aws_security_group.app.id
}

output "migrate_task_definition" {
  value = aws_ecs_task_definition.migrate.family
}

output "api_token" {
  value     = var.api_token
  sensitive = true
}
