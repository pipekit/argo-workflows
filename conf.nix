let 
  version = "0.0.1";
in 
{
  staticFiles = true;
  version = "${version}";
  env = {
    DEFAULT_REQUEUE_TIME="1s";
    SECURE="false";
    ALWAYS_OFFLOAD_NODE_STATUS="false";
    LOG_LEVEL="debug";
    UPPERIO_DB_DEBUG="0";
    IMAGE_NAMESPACE="";
    VERSION="${version}";
    AUTH_MODE="";
    NAMESPACED="";
    NAMESPACE="";
    MANAGED_NAMESPACE="";
    CTRL="";
    LOGS="";
    UI="";
    API="";
    PLUGINS="";
  };
}
