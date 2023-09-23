(() => {
    let gonb_comm = globalThis?.gonb_comm;
    if (!gonb_comm) {
        console.error("Communication to GoNB not setup, button will not synchronize with program.")
        return;
    }
    let buttonCount = gonb_comm.newSyncedVariable("{{.Address}}", 0);
    const button = document.getElementById("{{.HtmlId}}");
    button.addEventListener("click", function() {
        buttonCount.set(buttonCount.get() + 1);
    });
})();
