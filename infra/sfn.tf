locals {
  lambdas = ["reserve", "fx", "compliance", "bank", "capture", "release", "cancelfx", "notify", "recon"]
}

data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "${local.name}-lambda"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "lambda_vpc" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

resource "aws_cloudwatch_log_group" "lambda" {
  for_each          = toset(local.lambdas)
  name              = "/aws/lambda/${local.name}-${each.key}"
  retention_in_days = 1
}

# Zips are built by scripts/build-lambdas.sh (GOOS=linux GOARCH=arm64, file mode 0755).
resource "aws_lambda_function" "step" {
  for_each         = toset(local.lambdas)
  function_name    = "${local.name}-${each.key}"
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  handler          = "bootstrap"
  filename         = "${path.module}/build/${each.key}.zip"
  source_code_hash = filebase64sha256("${path.module}/build/${each.key}.zip")
  memory_size      = 256
  timeout          = 25

  vpc_config {
    subnet_ids         = aws_subnet.public[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = {
      DATABASE_URL = local.database_url
      MOCKS_URL    = "http://mocks.payout.local:8090" # Cloud Map private DNS: VPC Lambdas have no NAT/internet
    }
  }

  depends_on = [aws_cloudwatch_log_group.lambda, aws_iam_role_policy_attachment.lambda_vpc]
}

data "aws_iam_policy_document" "sfn_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["states.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "sfn" {
  name               = "${local.name}-sfn"
  assume_role_policy = data.aws_iam_policy_document.sfn_assume.json
}

resource "aws_iam_role_policy" "sfn_invoke" {
  role = aws_iam_role.sfn.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["lambda:InvokeFunction"]
      Resource = [for l in aws_lambda_function.step : l.arn]
    }]
  })
}

resource "aws_sfn_state_machine" "payout" {
  name     = "${local.name}-payout"
  role_arn = aws_iam_role.sfn.arn
  type     = "STANDARD"
  definition = templatefile("${path.module}/../sfn/payout.asl.json", {
    reserve_arn    = aws_lambda_function.step["reserve"].arn
    fx_arn         = aws_lambda_function.step["fx"].arn
    compliance_arn = aws_lambda_function.step["compliance"].arn
    bank_arn       = aws_lambda_function.step["bank"].arn
    capture_arn    = aws_lambda_function.step["capture"].arn
    release_arn    = aws_lambda_function.step["release"].arn
    cancelfx_arn   = aws_lambda_function.step["cancelfx"].arn
    notify_arn     = aws_lambda_function.step["notify"].arn
  })
}

# EventBridge Scheduler triggers the SFN-side reconciliation Lambda (the Temporal side uses a cron workflow).
data "aws_iam_policy_document" "scheduler_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "scheduler" {
  name               = "${local.name}-scheduler"
  assume_role_policy = data.aws_iam_policy_document.scheduler_assume.json
}

resource "aws_iam_role_policy" "scheduler_invoke" {
  role = aws_iam_role.scheduler.id
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = ["lambda:InvokeFunction"], Resource = [aws_lambda_function.step["recon"].arn] }]
  })
}

resource "aws_scheduler_schedule" "recon" {
  name                = "${local.name}-recon"
  schedule_expression = "rate(5 minutes)"
  flexible_time_window {
    mode = "OFF"
  }
  target {
    arn      = aws_lambda_function.step["recon"].arn
    role_arn = aws_iam_role.scheduler.arn
    input    = jsonencode({ source = "eventbridge-scheduler" })
  }
}
