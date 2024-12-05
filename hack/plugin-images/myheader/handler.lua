local MyHeader = {}

MyHeader.PRIORITY = 1000
MyHeader.VERSION = "1.0.0"

function MyHeader:header_filter(conf)
  -- do custom logic here
  kong.response.set_header("myheader", conf.header_value)
end

return MyHeader

