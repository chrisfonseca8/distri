output "services" {
  value = {
    for name, instance in aws_instance.service : name => {
      instance_id = instance.id
      private_ip  = instance.private_ip
      public_ip   = aws_eip.service[name].public_ip
    }
  }
}

output "broker" {
  value = {
    instance_id = aws_instance.broker.id
    private_ip  = aws_instance.broker.private_ip
    public_ip   = aws_eip.broker.public_ip
    data_volume = aws_ebs_volume.rabbitmq_data.id
  }
}

output "loadgen" {
  value = {
    instance_id = aws_instance.loadgen.id
    private_ip  = aws_instance.loadgen.private_ip
    public_ip   = aws_eip.loadgen.public_ip
  }
}

output "make_env_command" {
  value = "deploy/cloud/make-env.sh <docker-repo> <image-tag> ${aws_instance.service["orchestrator"].private_ip} ${aws_instance.service["order"].private_ip} ${aws_instance.service["inventory"].private_ip} ${aws_instance.service["payment"].private_ip} ${aws_instance.broker.private_ip} ${aws_instance.loadgen.private_ip}"
}

output "ssm_commands" {
  value = {
    orchestrator = "aws ssm start-session --target ${aws_instance.service["orchestrator"].id} --region ${var.aws_region}"
    order        = "aws ssm start-session --target ${aws_instance.service["order"].id} --region ${var.aws_region}"
    inventory    = "aws ssm start-session --target ${aws_instance.service["inventory"].id} --region ${var.aws_region}"
    payment      = "aws ssm start-session --target ${aws_instance.service["payment"].id} --region ${var.aws_region}"
    broker       = "aws ssm start-session --target ${aws_instance.broker.id} --region ${var.aws_region}"
    loadgen      = "aws ssm start-session --target ${aws_instance.loadgen.id} --region ${var.aws_region}"
  }
}
