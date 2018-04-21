set -o errexit
set -o pipefail
set -u

set -x
FP1_INSTALL_DIR_HOST="${FP1_INSTALL_DIR_HOST:-/usr/local/fp1}"
FP1_INSTALL_DIR_CONTAINER="${FP1_INSTALL_DIR_CONTAINER:-/var/paas/fp1}"
ROOT_MOUNT_DIR="${ROOT_MOUNT_DIR:-/root}"
set +x

RETCODE_SUCCESS=0
RETCODE_ERROR=1

update_container_ld_cache() {
  echo "Updating container's ld cache..."
  echo "${FP1_INSTALL_DIR_CONTAINER}/lib" > /etc/ld.so.conf.d/fp1.conf
  ldconfig
  echo "Updating container's ld cache... DONE."
}

configure_fp1_installation_dirs() {
  echo "Configuring installation directories..."
  mkdir -p "${ROOT_MOUNT_DIR}${FP1_INSTALL_DIR_HOST}"
  pushd "${ROOT_MOUNT_DIR}${FP1_INSTALL_DIR_HOST}"
  mkdir -p bin lib
  popd
  echo "Configuring installation directories... DONE."
}

run_fp1_installer() {
  echo "Running FP1 installer..."
  pushd "${FP1_INSTALL_DIR_CONTAINER}"
  sh fpga_tool_setup.sh
  mv tools/fpga_tool/dist/FpgaCmdEntry "${ROOT_MOUNT_DIR}${FP1_INSTALL_DIR_HOST}/bin"
  popd
  pushd "${FP1_INSTALL_DIR_CONTAINER}/software/kernel_drivers/xdma_driver/driver/xclng/xdma"
  make install
  popd
  pushd "${FP1_INSTALL_DIR_CONTAINER}/software/userspace/dpdk_src"
  sh build_dpdk.sh
  mv dpdk-16.04/build/lib/* "${ROOT_MOUNT_DIR}${FP1_INSTALL_DIR_HOST}/lib"
  mv securec/lib/* "${ROOT_MOUNT_DIR}${FP1_INSTALL_DIR_HOST}/lib"
  popd
  echo "Running FP1 installer... DONE."
}

verify_fp1_installation() {
  echo "Verifying FP1 installation..."
  echo "Verifying FP1 installation... DONE."
}

update_host_ld_cache() {
  echo "Updating host's ld cache..."
  echo "${FP1_INSTALL_DIR_HOST}/lib" >> "${ROOT_MOUNT_DIR}/etc/ld.so.conf"
  ldconfig -r "${ROOT_MOUNT_DIR}"
  echo "Updating host's ld cache... DONE."
}

main() {
  configure_fp1_installation_dirs
  run_fp1_installer
  verify_fp1_installation
  update_host_ld_cache
}

main "$@"
