(() => {
    let gonb_comm = globalThis?.gonb_comm;
    if (!gonb_comm) {
        console.error("Communication to GoNB not setup, slider will not synchronize with program.")
        return;
    }
    let sliderValue = gonb_comm.newSyncedVariable("{{.Address}}", 0);
    const slider = document.getElementById("{{.HtmlId}}");
    slider.addEventListener("change", function() {
        slider.setAttribute("value", slider.value);
        sliderValue.set(slider.value);
    });
})();
