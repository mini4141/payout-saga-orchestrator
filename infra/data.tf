resource "aws_ecr_repository" "app" {
  name         = local.name
  force_delete = true # images are removed with the repository on destroy

  image_scanning_configuration {
    scan_on_push = false
  }
}

resource "random_password" "db" {
  length  = 24
  special = false
}

resource "aws_db_subnet_group" "main" {
  name       = local.name
  subnet_ids = aws_subnet.public[*].id
}

resource "aws_db_instance" "main" {
  identifier                 = local.name
  engine                     = "postgres"
  engine_version             = "17"
  instance_class             = "db.t4g.micro"
  allocated_storage          = 20
  storage_type               = "gp3"
  db_name                    = "payout"
  username                   = "payout"
  password                   = random_password.db.result
  db_subnet_group_name       = aws_db_subnet_group.main.name
  vpc_security_group_ids     = [aws_security_group.db.id]
  publicly_accessible        = false
  multi_az                   = false
  storage_encrypted          = true
  backup_retention_period    = 0
  skip_final_snapshot        = true
  deletion_protection        = false
  apply_immediately          = true
  auto_minor_version_upgrade = false
}

locals {
  database_url = "postgres://payout:${random_password.db.result}@${aws_db_instance.main.address}:5432/payout?sslmode=require"
}
