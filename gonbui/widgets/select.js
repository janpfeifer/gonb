(() => {
    let gonb_comm = globalThis?.gonb_comm;
    if (!gonb_comm) {
        console.error("Communication to GoNB not setup, 'select' will not synchronize with program.")
        return;
    }
    const el = document.getElementById("{{.HtmlId}}");
    let selectValue = gonb_comm.newSyncedVariable("{{.Address}}", el.value);
    console.log("Listening to address: {{.Address}}");
    el.addEventListener("change", function() {
        // Called when user finishes interaction.
        el.setAttribute("value", el.value);  // Makes value available when reading `outerHTML`.
        selectValue.set(el.value);
    });
    el.addEventListener("input", function() {
        // Called while select is being changed.
        selectValue.set(el.value);
    });
    selectValue.subscribe((value) => {
        el.value = value;
        el.setAttribute("value", el.value); // Makes value available when reading `outerHTML`.
    })
})();
