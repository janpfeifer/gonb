(() => {
    let gonb_comm = globalThis?.gonb_comm;
    if (!gonb_comm) {
        console.error("Communication to GoNB not setup, slider will not synchronize with program.")
        return;
    }
    const slider = document.getElementById("{{.HtmlId}}");
    let sliderValue = gonb_comm.newSyncedVariable("{{.Address}}", slider.value);
    slider.addEventListener("change", function() {
        slider.setAttribute("value", slider.value);  // Makes value available when reading `outerHTML`.
        sliderValue.set(slider.value);
    });
    sliderValue.subscribe((value) => {
        slider.value = value;
        slider.setAttribute("value", slider.value); // Makes value available when reading `outerHTML`.
    })
})();
