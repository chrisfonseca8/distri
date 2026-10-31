data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  filter {
    name   = "root-device-type"
    values = ["ebs"]
  }
}

resource "aws_vpc" "staging" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_internet_gateway" "staging" {
  vpc_id = aws_vpc.staging.id
  tags   = { Name = "${var.project_name}-igw" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.staging.id
  cidr_block              = var.public_subnet_cidr
  availability_zone       = var.availability_zone
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.staging.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.staging.id
  }

  tags = { Name = "${var.project_name}-public" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

resource "aws_security_group" "app" {
  name        = "${var.project_name}-services"
  description = "Separated application service hosts"
  vpc_id      = aws_vpc.staging.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-services" }
}

resource "aws_security_group" "broker" {
  name        = "${var.project_name}-broker"
  description = "RabbitMQ broker"
  vpc_id      = aws_vpc.staging.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-broker" }
}

resource "aws_security_group" "loadgen" {
  name        = "${var.project_name}-loadgen"
  description = "Load generator and gateway simulator"
  vpc_id      = aws_vpc.staging.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-loadgen" }
}

resource "aws_vpc_security_group_ingress_rule" "app_from_loadgen" {
  security_group_id            = aws_security_group.app.id
  referenced_security_group_id = aws_security_group.loadgen.id
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8083
  description                  = "Load generator to app APIs and telemetry"
}

resource "aws_vpc_security_group_ingress_rule" "service_to_service" {
  security_group_id            = aws_security_group.app.id
  referenced_security_group_id = aws_security_group.app.id
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8083
  description                  = "Orchestrator to inventory and future internal API traffic"
}

resource "aws_vpc_security_group_ingress_rule" "app_ssh_from_loadgen" {
  security_group_id            = aws_security_group.app.id
  referenced_security_group_id = aws_security_group.loadgen.id
  ip_protocol                  = "tcp"
  from_port                    = 22
  to_port                      = 22
  description                  = "Remote Docker socket SSH tunnel"
}

resource "aws_vpc_security_group_ingress_rule" "broker_amqp_from_app" {
  security_group_id            = aws_security_group.broker.id
  referenced_security_group_id = aws_security_group.app.id
  ip_protocol                  = "tcp"
  from_port                    = 5672
  to_port                      = 5672
  description                  = "Application AMQP"
}

resource "aws_vpc_security_group_ingress_rule" "broker_ui_from_loadgen" {
  security_group_id            = aws_security_group.broker.id
  referenced_security_group_id = aws_security_group.loadgen.id
  ip_protocol                  = "tcp"
  from_port                    = 15672
  to_port                      = 15672
  description                  = "Load-test RabbitMQ metrics"
}

resource "aws_vpc_security_group_ingress_rule" "broker_ssh_from_loadgen" {
  security_group_id            = aws_security_group.broker.id
  referenced_security_group_id = aws_security_group.loadgen.id
  ip_protocol                  = "tcp"
  from_port                    = 22
  to_port                      = 22
  description                  = "Load-generator bastion to broker SSH"
}

resource "aws_vpc_security_group_ingress_rule" "simulator_from_app" {
  security_group_id            = aws_security_group.loadgen.id
  referenced_security_group_id = aws_security_group.app.id
  ip_protocol                  = "tcp"
  from_port                    = 9090
  to_port                      = 9090
  description                  = "Payment service to gateway simulator"
}

resource "aws_vpc_security_group_ingress_rule" "admin_ssh" {
  for_each = var.admin_cidr == "" ? {} : {
    app     = aws_security_group.app.id
    broker  = aws_security_group.broker.id
    loadgen = aws_security_group.loadgen.id
  }

  security_group_id = each.value
  cidr_ipv4         = var.admin_cidr
  ip_protocol       = "tcp"
  from_port         = 22
  to_port           = 22
  description       = "Administrator SSH"
}

resource "aws_vpc_security_group_ingress_rule" "admin_rabbit_ui" {
  count = var.admin_cidr == "" ? 0 : 1

  security_group_id = aws_security_group.broker.id
  cidr_ipv4         = var.admin_cidr
  ip_protocol       = "tcp"
  from_port         = 15672
  to_port           = 15672
  description       = "Administrator RabbitMQ UI"
}

resource "aws_vpc_security_group_ingress_rule" "additional_admin_ssh" {
  for_each = {
    for pair in setproduct(
      [
        { name = "app", group_id = aws_security_group.app.id },
        { name = "broker", group_id = aws_security_group.broker.id },
        { name = "loadgen", group_id = aws_security_group.loadgen.id },
      ],
      var.additional_admin_cidrs,
      ) : "${pair[0].name}-${replace(pair[1], "/", "-")}" => {
      group_id = pair[0].group_id
      cidr     = pair[1]
    }
  }

  security_group_id = each.value.group_id
  cidr_ipv4         = each.value.cidr
  ip_protocol       = "tcp"
  from_port         = 22
  to_port           = 22
  description       = "Additional administrator SSH address"
}

resource "aws_vpc_security_group_ingress_rule" "additional_admin_rabbit_ui" {
  for_each = var.additional_admin_cidrs

  security_group_id = aws_security_group.broker.id
  cidr_ipv4         = each.value
  ip_protocol       = "tcp"
  from_port         = 15672
  to_port           = 15672
  description       = "Additional administrator RabbitMQ UI address"
}

resource "aws_iam_role" "instance" {
  count = var.create_instance_profile ? 1 : 0

  name = "${var.project_name}-instance"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  count = var.create_instance_profile ? 1 : 0

  role       = aws_iam_role.instance[0].name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "instance" {
  count = var.create_instance_profile ? 1 : 0

  name = "${var.project_name}-instance"
  role = aws_iam_role.instance[0].name
}

locals {
  common_bootstrap = templatefile("${path.module}/templates/bootstrap.sh.tftpl", {
    compose_version = var.docker_compose_version
  })
  services = {
    orchestrator = 10
    order        = 11
    inventory    = 12
    payment      = 13
  }
}

resource "aws_instance" "service" {
  for_each                    = local.services
  ami                         = data.aws_ami.al2023.id
  instance_type               = var.service_instance_type
  availability_zone           = var.availability_zone
  subnet_id                   = aws_subnet.public.id
  private_ip                  = cidrhost(var.public_subnet_cidr, each.value)
  associate_public_ip_address = true
  vpc_security_group_ids      = [aws_security_group.app.id]
  iam_instance_profile        = var.create_instance_profile ? aws_iam_instance_profile.instance[0].name : null
  key_name                    = var.key_name
  monitoring                  = true
  user_data_replace_on_change = true
  user_data                   = local.common_bootstrap

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  root_block_device {
    volume_type           = "gp3"
    volume_size           = var.service_root_gib
    iops                  = 3000
    throughput            = 125
    encrypted             = true
    delete_on_termination = true
  }

  tags = {
    Name    = "${var.project_name}-${each.key}"
    Service = each.key
  }
}

resource "aws_ebs_volume" "rabbitmq_data" {
  availability_zone = var.availability_zone
  type              = "gp3"
  size              = var.broker_data_gib
  iops              = 3000
  throughput        = 125
  encrypted         = true

  tags = { Name = "${var.project_name}-rabbitmq-data" }

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_instance" "broker" {
  ami                         = data.aws_ami.al2023.id
  instance_type               = var.broker_instance_type
  availability_zone           = var.availability_zone
  subnet_id                   = aws_subnet.public.id
  private_ip                  = cidrhost(var.public_subnet_cidr, 20)
  associate_public_ip_address = true
  vpc_security_group_ids      = [aws_security_group.broker.id]
  iam_instance_profile        = var.create_instance_profile ? aws_iam_instance_profile.instance[0].name : null
  key_name                    = var.key_name
  monitoring                  = true
  user_data_replace_on_change = true
  user_data = templatefile("${path.module}/templates/broker-bootstrap.sh.tftpl", {
    compose_version  = var.docker_compose_version
    volume_id_nodash = replace(aws_ebs_volume.rabbitmq_data.id, "-", "")
  })

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  root_block_device {
    volume_type           = "gp3"
    volume_size           = var.broker_root_gib
    iops                  = 3000
    throughput            = 125
    encrypted             = true
    delete_on_termination = true
  }

  tags = { Name = "${var.project_name}-broker" }
}

resource "aws_volume_attachment" "rabbitmq_data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.rabbitmq_data.id
  instance_id = aws_instance.broker.id
}

resource "aws_instance" "loadgen" {
  ami                         = data.aws_ami.al2023.id
  instance_type               = var.loadgen_instance_type
  availability_zone           = var.availability_zone
  subnet_id                   = aws_subnet.public.id
  private_ip                  = cidrhost(var.public_subnet_cidr, 30)
  associate_public_ip_address = true
  vpc_security_group_ids      = [aws_security_group.loadgen.id]
  iam_instance_profile        = var.create_instance_profile ? aws_iam_instance_profile.instance[0].name : null
  key_name                    = var.key_name
  monitoring                  = true
  user_data_replace_on_change = true
  user_data                   = local.common_bootstrap

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  root_block_device {
    volume_type           = "gp3"
    volume_size           = var.loadgen_root_gib
    iops                  = 3000
    throughput            = 125
    encrypted             = true
    delete_on_termination = true
  }

  tags = { Name = "${var.project_name}-loadgen" }
}

resource "aws_eip" "service" {
  for_each = var.create_elastic_ips ? aws_instance.service : {}
  domain   = "vpc"
  instance = each.value.id
  tags     = { Name = "${var.project_name}-${each.key}" }
}

resource "aws_eip" "broker" {
  count = var.create_elastic_ips ? 1 : 0

  domain   = "vpc"
  instance = aws_instance.broker.id
  tags     = { Name = "${var.project_name}-broker" }
}

resource "aws_eip" "loadgen" {
  count = var.create_elastic_ips ? 1 : 0

  domain   = "vpc"
  instance = aws_instance.loadgen.id
  tags     = { Name = "${var.project_name}-loadgen" }
}

resource "aws_budgets_budget" "staging" {
  count = var.budget_alert_email == "" ? 0 : 1

  name         = "${var.project_name}-monthly"
  budget_type  = "COST"
  limit_amount = tostring(var.monthly_budget_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.budget_alert_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.budget_alert_email]
  }
}
