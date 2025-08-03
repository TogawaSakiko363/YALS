# YALS 用户设置脚本

此脚本用于在Linux系统上自动检测并安装所需依赖，并创建具有最低命令授权的Linux用户。

## 功能

1. 自动检测系统类型（支持Debian/Ubuntu、CentOS/RHEL/Fedora、Arch Linux和Alpine Linux）
2. 检查并安装必要的依赖：
   - curl（从系统软件源安装）
   - mtr（从系统软件源安装）
   - nexttrace（通过执行`curl nxtrace.org/nt | bash`安装）
3. 创建具有最低命令授权的Linux用户：
   - 用户名：yals
   - 密码：16位随机字符（脚本执行完成后显示）
   - 授权命令：ping、mtr、nexttrace

## 使用方法

1. 将脚本上传到目标Linux服务器
2. 赋予脚本执行权限：
   ```
   chmod +x setup_yals_user.sh
   ```
3. 以root权限运行脚本：
   ```
   sudo ./setup_yals_user.sh
   ```
4. 脚本执行完成后，会显示创建的用户名和随机生成的密码

## 注意事项

- 脚本需要root权限才能执行
- 如果用户已存在，脚本将重置密码和权限
- 请妥善保存生成的密码信息
- 脚本通过sudoers配置允许yals用户无需密码执行ping、mtr和nexttrace命令