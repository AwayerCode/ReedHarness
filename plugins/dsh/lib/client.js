window.__ModuleLoader__.load({
  id: "dsh-harn",
  factory: require => {
    const React = require("react");
    function Architecture(props) {
      const cwd = props.useSessions(state => state.byId?.[props.sessionId || state.current]?.cwd || "");
      const [source, setSource] = React.useState("");
      const [error, setError] = React.useState("");
      React.useEffect(() => {
        const controller = new AbortController();
        setSource("");
        setError("");
        fetch("/harn/resolve?cwd=" + encodeURIComponent(cwd), { signal: controller.signal }).then(async response => {
          if (!response.ok) throw new Error(await response.text());
          return response.json();
        }).then(info => {
          if (!controller.signal.aborted) setSource(info.url);
        }).catch(error => { if (!controller.signal.aborted) setError(error.message); });
        return () => controller.abort();
      }, [cwd]);
      if (error) return React.createElement("pre", { role: "alert" }, error);
      if (!source) return React.createElement("p", null, "正在连接 Harn…");
      return React.createElement("iframe", { title: "Harn 架构", src: source, style: { width: "100%", height: "100%", border: 0 } });
    }
    return {
      inject: ["slots"],
      apply(ctx) {
        ctx.slots.inject("conversation.view", () => ctx.slots.register({ name: "conversation.view", id: "harn", order: 21, label: () => "架构", inject: () => ({}) }, Architecture));
      },
    };
  },
});
